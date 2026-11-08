export const token = new URLSearchParams(window.location.search).get('token') ?? '';

/** Calls a viewer API, adding the access token from the page URL. */
export async function api(path, options = {}) {
  const url = new URL(path, window.location.origin);
  url.searchParams.set('token', token);
  const response = await fetch(url, options);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error ?? `${response.status} ${response.statusText}`);
  }
  return response.status === 204 ? null : response.json();
}
