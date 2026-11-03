package api

import "testing"

func TestResolveActorIdentity(t *testing.T) {
	s := func(v string) *string { return &v }

	tests := []struct {
		name       string
		email      *string // users.email via actor_type='user'
		tokenName  *string // deploy_tokens.name
		tokenOwner *string // users.email via deploy_tokens.created_by
		actorID    string
		wantLabel  string
		wantVia    string
	}{
		{
			name:      "user row shows its own email and no via",
			email:     s("mi.zhou@longbridge.sg"),
			actorID:   "b0e7aebd-1304-40a4-ad90-682c506e89a4",
			wantLabel: "mi.zhou@longbridge.sg",
			wantVia:   "",
		},
		{
			// The regression this whole change exists for: the row used to render
			// as "oidc-session-2026-08-11", a name shared by everyone who logged
			// in that day, so the action looked attributable but wasn't.
			name:       "token with a known issuer attributes to the person",
			tokenName:  s("oidc-session-2026-08-11"),
			tokenOwner: s("tao.zhou@longbridge-inc.com"),
			actorID:    "f7e11c69-0000-0000-0000-000000000000",
			wantLabel:  "tao.zhou@longbridge-inc.com",
			wantVia:    "oidc-session-2026-08-11",
		},
		{
			// bootstrap-dev / dev-cli are minted by the control plane with no
			// user context, so created_by is NULL. There is no person to name.
			name:      "machine-minted token has no issuer, name becomes the label",
			tokenName: s("bootstrap-dev"),
			actorID:   "e9b8cf41-0000-0000-0000-000000000000",
			wantLabel: "bootstrap-dev",
			wantVia:   "", // must not repeat the label
		},
		{
			// Token row survives the token it points at (project delete cascades
			// deploy_tokens); both joins miss and only the raw id is left.
			name:      "unresolvable row falls back to the actor id",
			actorID:   "71a7308d-0000-0000-0000-000000000000",
			wantLabel: "71a7308d-0000-0000-0000-000000000000",
			wantVia:   "",
		},
		{
			name:      "empty strings are treated as absent, not as an identity",
			email:     s(""),
			tokenName: s(""),
			actorID:   "00000000-0000-0000-0000-000000000000",
			wantLabel: "00000000-0000-0000-0000-000000000000",
			wantVia:   "",
		},
		{
			// A user-kind row never carries token columns, but if the data is
			// ever mixed the person must still win over any token name.
			name:       "email wins over token columns",
			email:      s("admin@example.com"),
			tokenName:  s("oidc-session-2026-08-11"),
			tokenOwner: s("someone.else@example.com"),
			actorID:    "aaaaaaaa-0000-0000-0000-000000000000",
			wantLabel:  "admin@example.com",
			wantVia:    "",
		},
		{
			// created_by is set but the token row itself is gone — keep the
			// person rather than dropping to the UUID.
			name:       "issuer without a token name still attributes",
			tokenOwner: s("tao.zhou@longbridge-inc.com"),
			actorID:    "bbbbbbbb-0000-0000-0000-000000000000",
			wantLabel:  "tao.zhou@longbridge-inc.com",
			wantVia:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label, via := resolveActorIdentity(tc.email, tc.tokenName, tc.tokenOwner, tc.actorID)
			if label != tc.wantLabel {
				t.Errorf("label = %q, want %q", label, tc.wantLabel)
			}
			if via != tc.wantVia {
				t.Errorf("via = %q, want %q", via, tc.wantVia)
			}
			if via != "" && via == label {
				t.Errorf("via %q duplicates the label; the UI would print it twice", via)
			}
		})
	}
}
