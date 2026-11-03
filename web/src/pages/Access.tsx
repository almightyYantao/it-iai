import { PageContainer } from "../components/Page";
import { ProjectAccessManager } from "../components/ProjectAccessManager";
import { ProxyStatusCard } from "../components/EgressProxy";

export function Access() {
  return (
    <PageContainer>
      <ProxyStatusCard />
      <ProjectAccessManager />
    </PageContainer>
  );
}
