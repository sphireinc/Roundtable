"use client";
export default function Error({ reset }: { error: Error & { digest?: string }; reset: () => void }) { return <main className="state-screen" role="alert"><p className="eyebrow">ROUNDTABLE / ERROR</p><h1>This view needs a retry</h1><p className="muted">The page could not render authoritative data.</p><button className="button" onClick={reset}>Retry</button></main>; }
