"use client";
export default function DashboardError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <main className="state-screen" role="alert"><p className="eyebrow">ROUNDTABLE / DASHBOARD ERROR</p><h1>Dashboard data is unavailable</h1><p className="muted">The rest of the application can remain open while this view retries the authoritative request.</p><button className="button" onClick={reset}>Retry dashboard</button></main>;
}
