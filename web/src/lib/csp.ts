// CONTENT SECURITY POLICY

// These helpers run when the app is built, and they write the rule that says what the finished page may load.

// apiOrigin gives back the scheme and host of the API address, or null when the API shares the address of the page.
export function apiOrigin(apiUrl: string | undefined): string | null {
  const trimmed = (apiUrl ?? '').trim()
  if (trimmed === '') {
    return null
  }
  try {
    const url = new URL(trimmed)
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.origin : null
  } catch {
    return null
  }
}

// inlineScripts finds the text of every script that is written straight into the page.
export function inlineScripts(html: string): string[] {
  const found: string[] = []
  for (const match of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)) {
    const [, attributes = '', body = ''] = match
    if (!/\bsrc\s*=/i.test(attributes) && body.trim() !== '') {
      found.push(body)
    }
  }
  return found
}

// scriptHash is the fingerprint a browser uses to allow one exact inline script and no other.
export async function scriptHash(script: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(script))
  return `'sha256-${btoa(String.fromCharCode(...new Uint8Array(digest)))}'`
}

// contentSecurityPolicy lets the page load only its own files, and talk only to itself and to the API.
export function contentSecurityPolicy(origin: string | null, scriptHashes: readonly string[]): string {
  const connect = origin === null ? "'self'" : `'self' ${origin}`
  return [
    "default-src 'self'",
    ["script-src 'self'", ...scriptHashes].join(' '),
    "style-src 'self'",
    // React and the charts set sizes and positions through style attributes, which a hash cannot cover.
    "style-src-attr 'unsafe-inline'",
    "img-src 'self' data:",
    "font-src 'self'",
    `connect-src ${connect}`,
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
  ].join('; ')
}

// withPolicy writes the policy into the head of the page, before anything that could load a file.
export function withPolicy(html: string, policy: string): string {
  const tag = `<meta http-equiv="Content-Security-Policy" content="${policy.replaceAll('"', '&quot;')}" />`
  return html.replace(/<head>/i, `<head>\n    ${tag}`)
}

// applyPolicy does the whole job for one page: hash its inline scripts, build the policy, write it in.
export async function applyPolicy(html: string, apiUrl: string | undefined): Promise<string> {
  const hashes = await Promise.all(inlineScripts(html).map(scriptHash))
  return withPolicy(html, contentSecurityPolicy(apiOrigin(apiUrl), hashes))
}
