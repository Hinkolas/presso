// Generates a random password for new WordPress admin accounts.
const ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#%^&*-_=+';

export function generatePassword(length = 20): string {
	const bytes = new Uint32Array(length);
	crypto.getRandomValues(bytes);
	let out = '';
	for (const b of bytes) out += ALPHABET[b % ALPHABET.length];
	return out;
}
