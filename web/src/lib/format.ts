// Display helpers: byte sizes, relative and absolute times, durations.

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];

export function formatBytes(n: number | null | undefined): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	let v = n;
	let i = 0;
	while (v >= 1024 && i < UNITS.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${UNITS[i]}`;
}

export function formatMB(bytes: number): string {
	return `${Math.round(bytes / (1024 * 1024))} MB`;
}

export function formatRelative(iso: string | null | undefined, now = Date.now()): string {
	if (!iso) return '–';
	const t = new Date(iso).getTime();
	if (isNaN(t)) return '–';
	const diff = now - t;
	const abs = Math.abs(diff);
	const suffix = diff >= 0 ? 'ago' : 'from now';
	const s = Math.round(abs / 1000);
	if (s < 45) return diff >= 0 ? 'just now' : 'in a moment';
	const m = Math.round(s / 60);
	if (m < 60) return `${m} min ${suffix}`;
	const h = Math.round(m / 60);
	if (h < 24) return `${h} h ${suffix}`;
	const d = Math.round(h / 24);
	if (d < 30) return `${d} d ${suffix}`;
	const mo = Math.round(d / 30);
	if (mo < 12) return `${mo} mo ${suffix}`;
	return `${Math.round(mo / 12)} y ${suffix}`;
}

export function formatDateTime(iso: string | null | undefined): string {
	if (!iso) return '–';
	const d = new Date(iso);
	if (isNaN(d.getTime())) return '–';
	return d.toLocaleString(undefined, {
		year: 'numeric',
		month: 'short',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit'
	});
}

/** "Oct 6, 2026" */
export function formatDate(iso: string | null | undefined): string {
	if (!iso) return '–';
	const d = new Date(iso);
	if (isNaN(d.getTime())) return '–';
	return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

export function formatTime(iso: string): string {
	const d = new Date(iso);
	if (isNaN(d.getTime())) return iso;
	return d.toLocaleTimeString(undefined, { hour12: false });
}

/** Milliseconds → "850 ms", "3.2 s", "2 min 5 s", "1 h 3 min". */
export function formatDuration(ms: number | null | undefined): string {
	if (ms === null || ms === undefined || !isFinite(ms) || ms < 0) return '–';
	if (ms < 1000) return `${Math.round(ms)} ms`;
	const s = ms / 1000;
	if (s < 60) return `${s < 10 ? s.toFixed(1) : Math.round(s)} s`;
	const m = Math.floor(s / 60);
	if (m < 60) return `${m} min ${Math.round(s - m * 60)} s`;
	const h = Math.floor(m / 60);
	if (h < 24) return `${h} h ${m - h * 60} min`;
	const d = Math.floor(h / 24);
	return `${d} d ${h - d * 24} h`;
}

/** Seconds of uptime → "3 d 4 h 12 min". */
export function formatUptime(seconds: number): string {
	return formatDuration(seconds * 1000);
}

/** Duration between two timestamps, or from start until now when unfinished. */
export function spanMs(start: string | null, end: string | null, now = Date.now()): number | null {
	if (!start) return null;
	const a = new Date(start).getTime();
	const b = end ? new Date(end).getTime() : now;
	if (isNaN(a) || isNaN(b)) return null;
	return b - a;
}

export function percent(part: number, total: number): string {
	if (!total) return '–';
	return `${Math.round((part / total) * 100)}%`;
}

export function formatNumber(n: number | null | undefined): string {
	if (n === null || n === undefined || !isFinite(n)) return '–';
	return n.toLocaleString();
}
