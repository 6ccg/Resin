import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { lazy } from "react";
import { AppShell } from "../components/AppShell";
import { LoginPage } from "../features/auth/LoginPage";
import { RequireAuth } from "../features/auth/RequireAuth";

const DashboardPage = lazy(() => import("../features/dashboard/DashboardPage").then((m) => ({ default: m.DashboardPage })));
const EndpointsPage = lazy(() => import("../features/endpoints/EndpointsPage").then((m) => ({ default: m.EndpointsPage })));
const GeoIPPage = lazy(() => import("../features/geoip/GeoIPPage").then((m) => ({ default: m.GeoIPPage })));
const NodesPage = lazy(() => import("../features/nodes/NodesPage").then((m) => ({ default: m.NodesPage })));
const PlatformDetailPage = lazy(() => import("../features/platforms/PlatformDetailPage").then((m) => ({ default: m.PlatformDetailPage })));
const PlatformPage = lazy(() => import("../features/platforms/PlatformPage").then((m) => ({ default: m.PlatformPage })));
const RequestLogsPage = lazy(() => import("../features/requestLogs/RequestLogsPage").then((m) => ({ default: m.RequestLogsPage })));
const RulesPage = lazy(() => import("../features/rules/RulesPage").then((m) => ({ default: m.RulesPage })));
const SubscriptionPage = lazy(() => import("../features/subscriptions/SubscriptionPage").then((m) => ({ default: m.SubscriptionPage })));
const SystemConfigPage = lazy(() => import("../features/systemConfig/SystemConfigPage").then((m) => ({ default: m.SystemConfigPage })));

function NodesRoute() {
  const location = useLocation();
  return <NodesPage key={location.search} />;
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route
        element={
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        }
      >
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<DashboardPage />} />
        <Route path="/platforms" element={<PlatformPage />} />
        <Route path="/platforms/:platformId" element={<PlatformDetailPage />} />
        <Route path="/subscriptions" element={<SubscriptionPage />} />
        <Route path="/nodes" element={<NodesRoute />} />
        <Route path="/endpoints" element={<EndpointsPage />} />
        <Route path="/rules" element={<RulesPage />} />
        <Route path="/request-logs" element={<RequestLogsPage />} />
        <Route path="/resources" element={<GeoIPPage />} />
        <Route path="/system-config" element={<SystemConfigPage />} />
      </Route>

      <Route path="*" element={<Navigate to="/dashboard" replace />} />
    </Routes>
  );
}
