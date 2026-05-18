import { Navigate, Route, Routes, useLocation } from "react-router-dom";

import { Shell } from "./components/Shell";
import { Login } from "./pages/Login";
import { Overview } from "./pages/Overview";
import { Projects } from "./pages/Projects";
import { ProjectDetail } from "./pages/ProjectDetail";
import { DeploymentDetail } from "./pages/DeploymentDetail";
import { Audit } from "./pages/Audit";
import { Users } from "./pages/Users";
import { Skill } from "./pages/Skill";
import { Settings } from "./pages/Settings";
import { getToken } from "./lib/api";

function RequireAuth({ children }: { children: JSX.Element }) {
  const loc = useLocation();
  if (!getToken()) {
    return <Navigate to="/login" replace state={{ from: loc.pathname }} />;
  }
  return children;
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Shell />
          </RequireAuth>
        }
      >
        <Route index element={<Overview />} />
        <Route path="projects" element={<Projects />} />
        <Route path="projects/:slug" element={<ProjectDetail />} />
        <Route path="projects/:slug/deployments/:id" element={<DeploymentDetail />} />
        <Route path="audit" element={<Audit />} />
        <Route path="users" element={<Users />} />
        <Route path="settings" element={<Settings />} />
        <Route path="skill" element={<Skill />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
