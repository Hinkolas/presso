// Turns a raw user-agent string into something a person recognises:
// "Firefox on macOS", "Safari on iPhone", "curl", or the raw token for an
// API client such as "presso/0.1.0".

export function deviceName(ua: string | null | undefined): string {
	if (!ua) return 'Unknown client';
	const s = ua.trim();

	// Non-browser clients identify with a product token; keep it verbatim.
	if (!/Mozilla\//.test(s)) return s.split(/\s+/)[0]?.slice(0, 60) || 'Unknown client';

	let browser = 'Browser';
	if (/Edg\//.test(s)) browser = 'Edge';
	else if (/OPR\//.test(s)) browser = 'Opera';
	else if (/Firefox\//.test(s)) browser = 'Firefox';
	else if (/Chrome\//.test(s) && !/Chromium/.test(s)) browser = 'Chrome';
	else if (/Chromium\//.test(s)) browser = 'Chromium';
	else if (/Safari\//.test(s) && /Version\//.test(s)) browser = 'Safari';
	else if (/HeadlessChrome/.test(s)) browser = 'Headless Chrome';

	let os = '';
	if (/iPhone/.test(s)) os = 'iPhone';
	else if (/iPad/.test(s)) os = 'iPad';
	else if (/Android/.test(s)) os = 'Android';
	else if (/Mac OS X|Macintosh/.test(s)) os = 'macOS';
	else if (/Windows/.test(s)) os = 'Windows';
	else if (/CrOS/.test(s)) os = 'ChromeOS';
	else if (/Linux/.test(s)) os = 'Linux';

	return os ? `${browser} on ${os}` : browser;
}

/** Whether the client is a browser (a desktop icon) or a program (a terminal icon). */
export function isBrowser(ua: string | null | undefined): boolean {
	return !!ua && /Mozilla\//.test(ua);
}
