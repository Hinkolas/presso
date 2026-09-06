// Unauthenticated liveness probe for the container. hooks.server.ts only
// calls the API when a session cookie is present, so probes cost no upstream
// round trip.
export const GET = () => new Response('ok');
