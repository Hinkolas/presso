// Splits a WP-CLI command line into argv the way a POSIX shell would for the
// simple cases: whitespace separates, single and double quotes group, and a
// backslash escapes the next character outside single quotes.
export function splitArgs(input: string): string[] {
	const out: string[] = [];
	let cur = '';
	let has = false;
	let quote: '"' | "'" | null = null;
	for (let i = 0; i < input.length; i++) {
		const c = input[i];
		if (quote) {
			if (c === quote) {
				quote = null;
			} else if (c === '\\' && quote === '"' && i + 1 < input.length) {
				cur += input[++i];
			} else {
				cur += c;
			}
			continue;
		}
		if (c === '"' || c === "'") {
			quote = c;
			has = true;
		} else if (c === '\\' && i + 1 < input.length) {
			cur += input[++i];
			has = true;
		} else if (/\s/.test(c)) {
			if (has) out.push(cur);
			cur = '';
			has = false;
		} else {
			cur += c;
			has = true;
		}
	}
	if (has) out.push(cur);
	return out;
}
