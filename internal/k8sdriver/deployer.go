package k8sdriver

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	netv1 "k8s.io/api/networking/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Traefik's IPAllowList CR — created via the dynamic client so we don't
// have to vendor the Traefik types. The same shape works for Traefik v2 and
// v3 (which renamed IPWhiteList → IPAllowList but kept both for compat).
var middlewareGVR = schema.GroupVersionResource{
	Group:    "traefik.io",
	Version:  "v1alpha1",
	Resource: "middlewares",
}

// IngressRoute is the Traefik CRD for path-prefix routing with per-route
// middleware chains. We emit one IngressRoute per project alongside the
// existing native Ingress: the Ingress keeps owning the default `/` route
// (and cert-manager TLS bootstrap), while the IngressRoute adds higher-
// priority routes for path-rule overrides like /api/webhook/* → no_auth.
// Only emitted when the project has at least one path rule.
var ingressRouteGVR = schema.GroupVersionResource{
	Group:    "traefik.io",
	Version:  "v1alpha1",
	Resource: "ingressroutes",
}

// Name of the cluster-wide Middleware CRD that ForwardAuths to control-plane's
// /v1/_internal/verify-api-token. Created once at platform install time
// (deploy/install-platform.sh) so we just reference it by name here.
const apiTokenVerifyMiddlewareRef = "oauth2-proxy-iai-api-token-verify@kubernetescrd"

type Deployer struct {
	cs            *kubernetes.Clientset
	dyn           dynamic.Interface // for Traefik Middleware CRs we don't import directly
	baseDomain    string
	ingressClass  string
	// Name of the cert-manager ClusterIssuer to attach to Ingresses that opt
	// into TLS. Empty disables the annotation entirely — handy on dev clusters
	// where cert-manager isn't installed yet.
	tlsClusterIssuer string
	// Image hostname rewritten when control plane refers to a different host than what k3d uses.
	registryHostFromCluster string
}

type DeployerOpts struct {
	Kubeconfig              string
	BaseDomain              string
	IngressClass            string
	TLSClusterIssuer        string
	RegistryHostFromCluster string // e.g. "host.k3d.internal:5001"
}

func New(opts DeployerOpts) (*Deployer, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", opts.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Deployer{
		cs:                      cs,
		dyn:                     dyn,
		baseDomain:              opts.BaseDomain,
		ingressClass:            opts.IngressClass,
		tlsClusterIssuer:        opts.TLSClusterIssuer,
		registryHostFromCluster: opts.RegistryHostFromCluster,
	}, nil
}

type ApplyInput struct {
	Slug     string
	Image    string            // <registry>/<slug>:<dep_id> as known to Build Service
	Port     int
	Env      map[string]string // user env + injected
	CPU      string
	Memory   string
	// SQLite enables the Litestream pattern: emptyDir /data shared with a
	// sidecar that continuously replicates /data/app.db to the project's
	// S3 bucket. An init container restores from S3 on pod start. Requires
	// the S3_* env vars to be set (caller enforces this — usually because
	// Needs.S3 was true alongside Needs.SQLite).
	SQLite bool
}

// Litestream defaults — kept as package-level constants so the init / sidecar
// container helpers and the env injection in the reconciler stay aligned.
const (
	litestreamImage    = "litestream/litestream:0.3.13"
	sqliteVolumeName   = "sqlite-data"
	sqliteMountPath    = "/data"
	sqliteDefaultDB    = "/data/app.db"
	sqliteS3ObjectKey  = "app.db" // path under the project's S3 bucket
)

// litestreamScript builds a small shell snippet that:
//   * generates a one-off config at /tmp/lc.yml from the S3_* env vars
//     already present in the pod (injected from project_env)
//   * exec's the supplied litestream subcommand
// Inline config keeps us from having to manage a ConfigMap per project.
func litestreamScript(subcommand string) string {
	return `set -eu
SCHEME=http
[ "${S3_USE_SSL:-false}" = "true" ] && SCHEME=https
cat > /tmp/lc.yml <<EOF
dbs:
  - path: ` + sqliteDefaultDB + `
    replicas:
      - type: s3
        bucket: ${S3_BUCKET}
        path: ` + sqliteS3ObjectKey + `
        endpoint: ${SCHEME}://${S3_ENDPOINT}
        access-key-id: ${S3_ACCESS_KEY_ID}
        secret-access-key: ${S3_SECRET_ACCESS_KEY}
        force-path-style: true
EOF
exec ` + subcommand
}

func (d *Deployer) namespaceFor(slug string) string { return "proj-" + slug }

// EnsureNamespace creates the per-project ns if missing.
func (d *Deployer) EnsureNamespace(ctx context.Context, slug string) error {
	ns := d.namespaceFor(slug)
	_, err := d.cs.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierr.IsNotFound(err) {
		return err
	}
	_, err = d.cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
			Labels: map[string]string{
				"vibedeploy.io/managed": "true",
				"vibedeploy.io/slug":    slug,
			},
		},
	}, metav1.CreateOptions{})
	return err
}

// Apply creates or updates the Deployment, Service, Ingress, and Secret for a project.
func (d *Deployer) Apply(ctx context.Context, in ApplyInput) error {
	if err := d.EnsureNamespace(ctx, in.Slug); err != nil {
		return err
	}
	if err := d.applySecret(ctx, in); err != nil {
		return err
	}
	if err := d.applyDeployment(ctx, in); err != nil {
		return err
	}
	if err := d.applyService(ctx, in); err != nil {
		return err
	}
	if err := d.applyIngress(ctx, in); err != nil {
		return err
	}
	return nil
}

// AppEnvSecretInput is the smaller payload UpdateAppEnvSecret needs — just
// the slug and the env map. Callers that only want to push fresh env vars
// (e.g. project_env API after the user edits one) shouldn't have to fabricate
// the rest of ApplyInput's fields.
type AppEnvSecretInput struct {
	Slug string
	Env  map[string]string
}

// UpdateAppEnvSecret refreshes the project's app-env Secret with the supplied
// env map. K8s rolls the Deployment automatically when a Secret it references
// changes, so the running pod picks up the new values within one rollout
// without any redeploy on the iai side.
//
// Thin wrapper over the private applySecret path so the env API and the
// reconciler stay aligned on Secret shape (name, labels, type).
func (d *Deployer) UpdateAppEnvSecret(ctx context.Context, in AppEnvSecretInput) error {
	return d.applySecret(ctx, ApplyInput{Slug: in.Slug, Env: in.Env})
}

func (d *Deployer) applySecret(ctx context.Context, in ApplyInput) error {
	ns := d.namespaceFor(in.Slug)
	name := "app-env"
	data := map[string][]byte{}
	for k, v := range in.Env {
		data[k] = []byte(v)
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: appLabels(in.Slug)},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
	cur, err := d.cs.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if apierr.IsNotFound(err) {
		_, err = d.cs.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	sec.ResourceVersion = cur.ResourceVersion
	_, err = d.cs.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{})
	return err
}

func (d *Deployer) applyDeployment(ctx context.Context, in ApplyInput) error {
	ns := d.namespaceFor(in.Slug)
	name := "app"
	image := in.Image
	if d.registryHostFromCluster != "" {
		image = rewriteImageHost(image, d.registryHostFromCluster)
	}

	cpu := in.CPU
	if cpu == "" {
		cpu = "1"
	}
	mem := in.Memory
	if mem == "" {
		mem = "2Gi"
	}

	appContainer := corev1.Container{
		Name:  "app",
		Image: image,
		Ports: []corev1.ContainerPort{{ContainerPort: int32(in.Port)}},
		EnvFrom: []corev1.EnvFromSource{
			{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-env"}}},
		},
		Env: []corev1.EnvVar{
			{Name: "PORT", Value: fmt.Sprintf("%d", in.Port)},
			{Name: "VIBEDEPLOY_PROJECT", Value: in.Slug},
			{Name: "VIBEDEPLOY_URL", Value: fmt.Sprintf("https://%s.%s", in.Slug, d.baseDomain)},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("200Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(mem),
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(in.Port)},
			},
			InitialDelaySeconds: 2,
			PeriodSeconds:       3,
			FailureThreshold:    20,
		},
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{appContainer},
	}

	// SQLite + Litestream wiring. When the manifest says needs.sqlite, the
	// pod gets:
	//   * an emptyDir volume at /data shared between init / app / sidecar
	//   * an init container that restores /data/app.db from S3 (no-op on
	//     first deploy, populates on subsequent restarts / node drains)
	//   * a sidecar that runs `litestream replicate` against the same DB
	// All three containers reuse the app-env Secret so the S3 creds the
	// reconciler wrote there flow straight through.
	if in.SQLite {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name:         sqliteVolumeName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
		mount := corev1.VolumeMount{Name: sqliteVolumeName, MountPath: sqliteMountPath}

		// Append the mount + SQLITE_PATH env to the app container.
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, mount)
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, corev1.EnvVar{
			Name:  "SQLITE_PATH",
			Value: sqliteDefaultDB,
		})

		// Init container: restore from S3 if a previous replica exists.
		// `-if-replica-exists` makes the very first deploy (empty bucket) a no-op.
		podSpec.InitContainers = append(podSpec.InitContainers, corev1.Container{
			Name:  "litestream-restore",
			Image: litestreamImage,
			EnvFrom: []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-env"}}},
			},
			Command:      []string{"/bin/sh", "-c"},
			Args:         []string{litestreamScript("litestream restore -if-replica-exists -config /tmp/lc.yml " + sqliteDefaultDB)},
			VolumeMounts: []corev1.VolumeMount{mount},
		})

		// Sidecar: continuous replication while the app is running.
		podSpec.Containers = append(podSpec.Containers, corev1.Container{
			Name:  "litestream",
			Image: litestreamImage,
			EnvFrom: []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-env"}}},
			},
			Command:      []string{"/bin/sh", "-c"},
			Args:         []string{litestreamScript("litestream replicate -config /tmp/lc.yml")},
			VolumeMounts: []corev1.VolumeMount{mount},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("20m"),
					corev1.ResourceMemory: resource.MustParse("32Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		})
	}

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: appLabels(in.Slug)},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: appLabels(in.Slug)},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: appLabels(in.Slug)},
				Spec:       podSpec,
			},
		},
	}

	cur, err := d.cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if apierr.IsNotFound(err) {
		_, err = d.cs.AppsV1().Deployments(ns).Create(ctx, dep, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	dep.ResourceVersion = cur.ResourceVersion
	_, err = d.cs.AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{})
	return err
}

func (d *Deployer) applyService(ctx context.Context, in ApplyInput) error {
	ns := d.namespaceFor(in.Slug)
	name := "app"
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: appLabels(in.Slug)},
		Spec: corev1.ServiceSpec{
			Selector: appLabels(in.Slug),
			Ports:    []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt(in.Port), Protocol: corev1.ProtocolTCP}},
			Type:     corev1.ServiceTypeClusterIP,
		},
	}
	cur, err := d.cs.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
	if apierr.IsNotFound(err) {
		_, err = d.cs.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	svc.ResourceVersion = cur.ResourceVersion
	svc.Spec.ClusterIP = cur.Spec.ClusterIP
	_, err = d.cs.CoreV1().Services(ns).Update(ctx, svc, metav1.UpdateOptions{})
	return err
}

func (d *Deployer) applyIngress(ctx context.Context, in ApplyInput) error {
	// Default ingress always contains the platform-issued subdomain. Custom
	// domains AND IP allow-list are reconciled by the caller via
	// SyncIngressHostnames after this returns, so we leave middleware empty here.
	defaultHost := fmt.Sprintf("%s.%s", in.Slug, d.baseDomain)
	return d.writeIngress(ctx, in.Slug, []string{defaultHost}, "")
}

// IngressHostsInput drives SyncIngressHostnames — the canonical list of all
// hostnames that should resolve to the project, plus the IP allow-list (CIDRs)
// the Ingress should restrict access to. Pass an empty / nil AllowCIDRs to
// mean "no restriction" — the Middleware will be removed if it exists.
type IngressHostsInput struct {
	Slug       string
	Hostnames  []string // include both the default subdomain and any custom domains
	AllowCIDRs []string // optional; empty = no IP restriction
	// RequireAuth attaches the cluster-wide oauth2-proxy ForwardAuth +
	// errors-redirect middlewares so unauthenticated requests are bounced
	// through Keycloak. Source from the project's visibility — public projects
	// pass through, org/restricted projects require login.
	RequireAuth bool
	// TLSEnabled adds a cert-manager.io/cluster-issuer annotation and a tls:
	// section to the Ingress. cert-manager solves an HTTP-01 challenge per
	// hostname and writes the cert into a per-project Secret that Traefik
	// reads from automatically.
	TLSEnabled bool
	// PathRules — when non-empty, an IngressRoute is emitted alongside the
	// default Ingress with one higher-priority route per rule. Each rule
	// overrides the project's default SSO gate for requests whose path starts
	// with PathPrefix. Empty / nil drops the IngressRoute if it existed.
	PathRules []PathRule
}

// PathRule mirrors model.ProjectPathRule but only the bits the deployer cares
// about — slug-scoped, no IDs or timestamps. Mode is one of "no_auth" or
// "token".
type PathRule struct {
	PathPrefix string
	Mode       string
}

// SyncIngressHostnames replaces the project's Ingress rules with one rule per
// supplied hostname. Idempotent; safe to call from domain add/remove handlers.
// Also reconciles the per-project IP-allowlist Middleware and chains the
// cluster-wide oauth2-proxy middlewares when RequireAuth is true.
func (d *Deployer) SyncIngressHostnames(ctx context.Context, in IngressHostsInput) error {
	if len(in.Hostnames) == 0 {
		return fmt.Errorf("SyncIngressHostnames: at least one hostname required")
	}
	// Reconcile the per-project IP-allowlist Middleware first; if it fails we
	// still want to write the Ingress so the project doesn't become unreachable.
	mwName, mwErr := d.syncIPAllowList(ctx, in.Slug, in.AllowCIDRs)
	if mwErr != nil {
		defer func() { _ = mwErr }()
	}

	// Build the ordered list of Middleware references for the annotation.
	// Order matters: Traefik applies them top-down on inbound requests.
	//   1. IP allow-list (drop early so unauthorised IPs never hit oauth2-proxy)
	//   2. errors-redirect (must wrap ForwardAuth so 401 → redirect)
	//   3. forward-auth   (the actual identity check)
	ns := d.namespaceFor(in.Slug)
	var refs []string
	if mwName != "" {
		refs = append(refs, fmt.Sprintf("%s-%s@kubernetescrd", ns, mwName))
	}
	if in.RequireAuth {
		refs = append(refs,
			"oauth2-proxy-errors-redirect@kubernetescrd",
			"oauth2-proxy-forward-auth@kubernetescrd",
		)
	}
	if err := d.writeIngressWithMiddlewareChain(ctx, in.Slug, in.Hostnames, refs, in.TLSEnabled); err != nil {
		return err
	}
	// Path-prefix overrides ride on a parallel IngressRoute. The Ingress above
	// stays the catch-all (and TLS source); the IngressRoute only matters when
	// rules are present. Failures here don't roll back the Ingress — owners
	// just lose the overrides; the default route still serves traffic.
	if err := d.syncPathRuleIngressRoute(ctx, in.Slug, in.Hostnames, in.PathRules); err != nil {
		return fmt.Errorf("sync path-rule IngressRoute: %w", err)
	}
	return nil
}

// syncPathRuleIngressRoute creates or deletes the per-project IngressRoute
// that carries path-prefix overrides. When rules is empty the IngressRoute is
// deleted so removing the last rule cleanly takes the routes out of traefik.
// Routes are ordered longest-prefix-first via explicit priorities so the most
// specific match wins regardless of insertion order.
func (d *Deployer) syncPathRuleIngressRoute(ctx context.Context, slug string, hosts []string, rules []PathRule) error {
	ns := d.namespaceFor(slug)
	name := "app-overrides"

	if len(rules) == 0 || len(hosts) == 0 {
		err := d.dyn.Resource(ingressRouteGVR).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierr.IsNotFound(err) {
			return fmt.Errorf("delete IngressRoute: %w", err)
		}
		return nil
	}

	// Sort rules longest-prefix-first so traefik's explicit priority picks the
	// most specific match even if the caller fed us rules in arbitrary order.
	ordered := append([]PathRule(nil), rules...)
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if len(ordered[j].PathPrefix) > len(ordered[i].PathPrefix) {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}

	// Quote each host for the traefik matcher. Traefik backtick-strings; we
	// fmt.Sprintf so the surrounding `…` are literal in the YAML.
	hostList := make([]string, 0, len(hosts))
	for _, h := range hosts {
		hostList = append(hostList, fmt.Sprintf("`%s`", h))
	}
	hostMatch := "Host(" + strings.Join(hostList, ", ") + ")"

	allowListMW := map[string]any{
		"name":      allowListMiddlewareName(slug),
		"namespace": ns,
	}

	routes := make([]any, 0, len(ordered))
	// Higher priority numbers win in traefik. Start high enough to beat the
	// default Ingress's implicit priority (which is len(host)+len(path) ≈ low
	// hundreds) and decrement by prefix length so longer prefixes still win
	// within the IngressRoute itself.
	const basePriority = 100_000
	for i, rule := range ordered {
		mws := []any{allowListMW}
		switch rule.Mode {
		case "token":
			// allow-list is in-project; api-token-verify is cluster-wide so we
			// need its namespace explicit in the middlewareRef.
			mws = append(mws, map[string]any{
				"name":      "iai-api-token-verify",
				"namespace": "oauth2-proxy",
			})
		case "no_auth":
			// no extra middleware — just the allow-list
		default:
			return fmt.Errorf("unknown path rule mode %q for %s", rule.Mode, rule.PathPrefix)
		}
		routes = append(routes, map[string]any{
			"match":    fmt.Sprintf("%s && PathPrefix(`%s`)", hostMatch, rule.PathPrefix),
			"kind":     "Rule",
			"priority": basePriority - i, // tie-break: earlier (longer) prefix wins
			"services": []any{map[string]any{
				"name": "app",
				"port": 80,
			}},
			"middlewares": mws,
		})
	}

	body := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "traefik.io/v1alpha1",
			"kind":       "IngressRoute",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
				"labels":    appLabels(slug),
			},
			"spec": map[string]any{
				"entryPoints": []any{"web", "websecure"},
				"routes":      routes,
			},
		},
	}

	cur, err := d.dyn.Resource(ingressRouteGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if apierr.IsNotFound(err) {
		_, err = d.dyn.Resource(ingressRouteGVR).Namespace(ns).Create(ctx, body, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create IngressRoute: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get IngressRoute: %w", err)
	}
	body.SetResourceVersion(cur.GetResourceVersion())
	_, err = d.dyn.Resource(ingressRouteGVR).Namespace(ns).Update(ctx, body, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update IngressRoute: %w", err)
	}
	return nil
}

// allowListMiddlewareName: stable per-project name so successive syncs update
// the same object rather than churning resources.
func allowListMiddlewareName(slug string) string { return "iai-allowlist-" + slug }

// syncIPAllowList creates or updates an IPAllowList Traefik Middleware for the
// project. Returns the Middleware name (empty when none should be attached).
// An empty cidrs slice causes the Middleware to be deleted.
func (d *Deployer) syncIPAllowList(ctx context.Context, slug string, cidrs []string) (string, error) {
	ns := d.namespaceFor(slug)
	name := allowListMiddlewareName(slug)

	// Drop empty entries and dedupe — operators sometimes paste blank lines.
	clean := make([]string, 0, len(cidrs))
	seen := map[string]struct{}{}
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		clean = append(clean, c)
	}

	if len(clean) == 0 {
		// Delete the Middleware so the Ingress goes back to "open access".
		err := d.dyn.Resource(middlewareGVR).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierr.IsNotFound(err) {
			return "", fmt.Errorf("delete IPAllowList middleware: %w", err)
		}
		return "", nil
	}

	cidrIface := make([]any, len(clean))
	for i, c := range clean {
		cidrIface[i] = c
	}

	body := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "traefik.io/v1alpha1",
			"kind":       "Middleware",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
				"labels": map[string]any{
					"app":                   slug,
					"vibedeploy.io/slug":    slug,
					"vibedeploy.io/managed": "true",
				},
			},
			"spec": map[string]any{
				"ipAllowList": map[string]any{
					"sourceRange": cidrIface,
				},
			},
		},
	}

	cur, err := d.dyn.Resource(middlewareGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if apierr.IsNotFound(err) {
		_, err = d.dyn.Resource(middlewareGVR).Namespace(ns).Create(ctx, body, metav1.CreateOptions{})
		if err != nil {
			return "", fmt.Errorf("create IPAllowList middleware: %w", err)
		}
		return name, nil
	}
	if err != nil {
		return "", fmt.Errorf("get IPAllowList middleware: %w", err)
	}
	body.SetResourceVersion(cur.GetResourceVersion())
	_, err = d.dyn.Resource(middlewareGVR).Namespace(ns).Update(ctx, body, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("update IPAllowList middleware: %w", err)
	}
	return name, nil
}

// writeIngress writes the project's Ingress. middlewareName, when non-empty,
// gets attached via the standard Traefik `router.middlewares` annotation in
// `<namespace>-<name>@kubernetescrd` form. Thin wrapper around the multi-
// middleware chain variant — kept so applyIngress (initial deploy, no auth /
// no allow-list yet) doesn't have to construct a slice.
func (d *Deployer) writeIngress(ctx context.Context, slug string, hosts []string, middlewareName string) error {
	var refs []string
	if middlewareName != "" {
		ns := d.namespaceFor(slug)
		refs = []string{fmt.Sprintf("%s-%s@kubernetescrd", ns, middlewareName)}
	}
	// applyIngress (the only caller) runs before the access reconciler reads
	// the project's tls flag, so we always emit the plain-HTTP form here. The
	// follow-up SyncIngressHostnames call rewrites the Ingress with TLS if
	// the project has the flag on.
	return d.writeIngressWithMiddlewareChain(ctx, slug, hosts, refs, false)
}

// writeIngressWithMiddlewareChain writes the project's Ingress, attaching the
// supplied middleware references (already fully-qualified —
// `<namespace>-<name>@kubernetescrd` or `<name>@<provider>`) as a single
// comma-joined `router.middlewares` annotation. Traefik applies them in order.
func (d *Deployer) writeIngressWithMiddlewareChain(ctx context.Context, slug string, hosts []string, middlewareRefs []string, tlsEnabled bool) error {
	ns := d.namespaceFor(slug)
	name := "app"
	pathType := netv1.PathTypePrefix

	rules := make([]netv1.IngressRule, 0, len(hosts))
	for _, h := range hosts {
		rules = append(rules, netv1.IngressRule{
			Host: h,
			IngressRuleValue: netv1.IngressRuleValue{
				HTTP: &netv1.HTTPIngressRuleValue{
					Paths: []netv1.HTTPIngressPath{{
						Path:     "/",
						PathType: &pathType,
						Backend: netv1.IngressBackend{
							Service: &netv1.IngressServiceBackend{
								Name: "app",
								Port: netv1.ServiceBackendPort{Number: 80},
							},
						},
					}},
				},
			},
		})
	}

	annotations := map[string]string{
		"traefik.ingress.kubernetes.io/router.entrypoints": "web,websecure",
	}
	if len(middlewareRefs) > 0 {
		annotations["traefik.ingress.kubernetes.io/router.middlewares"] = strings.Join(middlewareRefs, ",")
	}

	// Per-project HTTPS: annotate the Ingress so cert-manager solves an
	// HTTP-01 challenge against every host in the tls block and writes the
	// resulting cert into iai-tls-<slug>. Traefik serves it automatically
	// because the Ingress references that secret in spec.tls. If the operator
	// hasn't installed cert-manager, the annotation is inert and the project
	// stays reachable over plain HTTP — no breakage, just no cert.
	var tlsSpec []netv1.IngressTLS
	if tlsEnabled && d.tlsClusterIssuer != "" {
		annotations["cert-manager.io/cluster-issuer"] = d.tlsClusterIssuer
		tlsSpec = []netv1.IngressTLS{{
			Hosts:      append([]string(nil), hosts...),
			SecretName: "iai-tls-" + slug,
		}}
	}

	ing := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Labels:      appLabels(slug),
			Annotations: annotations,
		},
		Spec: netv1.IngressSpec{
			IngressClassName: &d.ingressClass,
			Rules:            rules,
			TLS:              tlsSpec,
		},
	}

	cur, err := d.cs.NetworkingV1().Ingresses(ns).Get(ctx, name, metav1.GetOptions{})
	if apierr.IsNotFound(err) {
		_, err = d.cs.NetworkingV1().Ingresses(ns).Create(ctx, ing, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	ing.ResourceVersion = cur.ResourceVersion
	_, err = d.cs.NetworkingV1().Ingresses(ns).Update(ctx, ing, metav1.UpdateOptions{})
	return err
}

func appLabels(slug string) map[string]string {
	return map[string]string{
		"app":                   slug,
		"vibedeploy.io/slug":    slug,
		"vibedeploy.io/managed": "true",
	}
}

// rewriteImageHost replaces the host part of an image reference.
// Builds inside docker-compose push to "registry:5000"; k3d nodes can't resolve that,
// so we rewrite to "host.k3d.internal:5001" before applying to the cluster.
func rewriteImageHost(image, newHost string) string {
	// image = "host:port/repo:tag" or "host/repo:tag"
	slash := strings.Index(image, "/")
	if slash < 0 {
		return image
	}
	return newHost + image[slash:]
}

// IsDeploymentReady returns true when at least one pod is ready.
func (d *Deployer) IsDeploymentReady(ctx context.Context, slug string) (bool, error) {
	ns := d.namespaceFor(slug)
	dep, err := d.cs.AppsV1().Deployments(ns).Get(ctx, "app", metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return dep.Status.ReadyReplicas > 0, nil
}

// PodSummary inspects the (latest) pod for a project and returns:
//   - summary:        short human-readable state line, suitable for an event log.
//   - ready:          true once at least one container has passed its readiness probe.
//   - terminalReason: non-empty when the pod has reached a state that won't recover
//                     without user intervention (ImagePullBackOff, CrashLoopBackOff,
//                     InvalidImageName, ...). The reconciler should fail the
//                     deployment with this reason instead of polling forever.
func (d *Deployer) PodSummary(ctx context.Context, slug string) (summary string, ready bool, terminalReason string, err error) {
	ns := d.namespaceFor(slug)
	pods, err := d.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app=" + slug})
	if err != nil {
		return "", false, "", err
	}
	if len(pods.Items) == 0 {
		return "pod not scheduled yet", false, "", nil
	}
	// We deploy with replicas=1 — pick the newest pod by creation time so we
	// don't latch onto a terminating one during rollouts.
	pod := pods.Items[0]
	for i := 1; i < len(pods.Items); i++ {
		if pods.Items[i].CreationTimestamp.After(pod.CreationTimestamp.Time) {
			pod = pods.Items[i]
		}
	}

	// Detect scheduling problems before container status is even populated.
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			if cond.Reason == "Unschedulable" {
				return fmt.Sprintf("unschedulable: %s", cond.Message), false,
					fmt.Sprintf("pod cannot be scheduled: %s", cond.Message), nil
			}
		}
	}

	if len(pod.Status.ContainerStatuses) == 0 {
		return fmt.Sprintf("pod %s (no container status yet)", pod.Status.Phase), false, "", nil
	}
	cs := pod.Status.ContainerStatuses[0]

	if cs.Ready {
		return "container ready", true, "", nil
	}

	switch {
	case cs.State.Waiting != nil:
		reason := cs.State.Waiting.Reason
		msg := strings.TrimSpace(cs.State.Waiting.Message)
		line := "waiting: " + reason
		if msg != "" {
			line += " — " + msg
		}
		// These are not going to fix themselves with more polling.
		switch reason {
		case "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "InvalidImageReference":
			detail := msg
			if detail == "" {
				detail = "k8s could not pull " + cs.Image
			}
			return line, false, "image pull failed: " + detail, nil
		case "CreateContainerConfigError", "CreateContainerError":
			detail := msg
			if detail == "" {
				detail = reason
			}
			return line, false, "container config error: " + detail, nil
		case "CrashLoopBackOff":
			detail := msg
			if detail == "" {
				detail = "container is restart-looping; check `kubectl logs`"
			}
			return line, false, "crash loop: " + detail, nil
		}
		return line, false, "", nil

	case cs.State.Running != nil:
		if cs.RestartCount >= 3 {
			return fmt.Sprintf("running but restarted %d times", cs.RestartCount), false,
				fmt.Sprintf("container has restarted %d times — likely failing its readiness probe or crashing post-start", cs.RestartCount), nil
		}
		return "container running, waiting for readiness probe", false, "", nil

	case cs.State.Terminated != nil:
		t := cs.State.Terminated
		line := fmt.Sprintf("terminated: %s (exit %d)", t.Reason, t.ExitCode)
		return line, false, fmt.Sprintf("container exited (%s, code %d): %s", t.Reason, t.ExitCode, strings.TrimSpace(t.Message)), nil
	}

	return fmt.Sprintf("pod %s", pod.Status.Phase), false, "", nil
}

// PodPlacement describes where a project's pod is physically running.
// Returned by GET /v1/projects/{slug} so the admin UI can show node assignment
// without the operator having to SSH and run kubectl.
type PodPlacement struct {
	Node    string `json:"node"`            // node the pod is scheduled on
	Pod     string `json:"pod"`             // pod name (useful for kubectl logs)
	Phase   string `json:"phase"`           // "Running" / "Pending" / ...
	Ready   bool   `json:"ready"`           // first container has passed readiness probe
	PodIP   string `json:"pod_ip,omitempty"`
	HostIP  string `json:"host_ip,omitempty"`
	Started string `json:"started,omitempty"` // RFC3339; empty if pod hasn't started yet
}

// PodPlacement returns where the (latest) pod for `slug` is currently running.
// nil + nil error means "no pod found" (project never deployed or namespace gone).
// Any cluster-read error is surfaced; callers may choose to swallow it for
// non-essential UI display.
func (d *Deployer) PodPlacement(ctx context.Context, slug string) (*PodPlacement, error) {
	ns := d.namespaceFor(slug)
	pods, err := d.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app=" + slug})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, nil
	}
	// Newest pod wins — same logic as PodSummary so the UI and the reconciler
	// stay in agreement on which pod they're talking about.
	pod := pods.Items[0]
	for i := 1; i < len(pods.Items); i++ {
		if pods.Items[i].CreationTimestamp.After(pod.CreationTimestamp.Time) {
			pod = pods.Items[i]
		}
	}
	out := &PodPlacement{
		Node:   pod.Spec.NodeName,
		Pod:    pod.Name,
		Phase:  string(pod.Status.Phase),
		PodIP:  pod.Status.PodIP,
		HostIP: pod.Status.HostIP,
	}
	if pod.Status.StartTime != nil {
		out.Started = pod.Status.StartTime.UTC().Format(time.RFC3339)
	}
	if len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].Ready {
		out.Ready = true
	}
	return out, nil
}

// TailLogs returns the last `tailLines` lines of the latest pod for the project.
func (d *Deployer) TailLogs(ctx context.Context, slug string, tailLines int64) (string, error) {
	ns := d.namespaceFor(slug)
	pods, err := d.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app=" + slug})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods for %s", slug)
	}
	pod := pods.Items[0].Name
	req := d.cs.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{TailLines: &tailLines})
	r, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var buf strings.Builder
	_, err = copyTo(&buf, r)
	return buf.String(), err
}

// copyTo is io.Copy on string builder; broken out so tests can stub.
func copyTo(dst *strings.Builder, src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 8192)
	var n int64
	for {
		nr, err := src.Read(buf)
		if nr > 0 {
			dst.Write(buf[:nr])
			n += int64(nr)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return n, nil
			}
			return n, err
		}
	}
}

// DeleteProject removes everything for a slug (namespace cascades children).
// Idempotent: a not-found namespace is treated as success so half-applied
// deletes can be retried.
func (d *Deployer) DeleteProject(ctx context.Context, slug string) error {
	ns := d.namespaceFor(slug)
	err := d.cs.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
	if err != nil && !apierr.IsNotFound(err) {
		return err
	}
	return nil
}
