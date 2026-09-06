// Turns a site name into a slug the engine accepts: lowercase ASCII letters,
// digits and single hyphens, trimmed to 40 characters.
export function slugify(input: string): string {
	return input
		.normalize('NFKD')
		.replace(/[\u0300-\u036f]/g, '')
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, '-')
		.replace(/^-+|-+$/g, '')
		.slice(0, 40)
		.replace(/-+$/g, '');
}
