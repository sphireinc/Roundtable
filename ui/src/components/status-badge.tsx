export function StatusBadge({ status }: { status: string }) { return <span className={`status status-${status}`}><span aria-hidden="true">●</span> {status}</span>; }
