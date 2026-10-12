import { describe, expect, it } from 'vitest'

import indexHtml from '../../index.html?raw'
import vercelJson from '../../vercel.json?raw'
import { apiOrigin, applyPolicy, contentSecurityPolicy, inlineScripts, scriptHash, withPolicy } from './csp'

// HELPERS

interface VercelHeader {
  key: string
  value: string
}
interface VercelConfig {
  headers?: { source: string; headers: VercelHeader[] }[]
}

// directive gives back the part of a policy that starts with the given name.
function directive(policy: string, name: string): string | undefined {
  return policy
    .split(';')
    .map((part) => part.trim())
    .find((part) => part === name || part.startsWith(`${name} `))
}

// TESTS

describe('apiOrigin', () => {
  it.each([
    ['https://shadow-pager-api.onrender.com', 'https://shadow-pager-api.onrender.com'],
    ['https://shadow-pager-api.onrender.com/', 'https://shadow-pager-api.onrender.com'],
    ['https://api.example.com/api/v1?x=1', 'https://api.example.com'],
    ['http://localhost:8080', 'http://localhost:8080'],
    ['  https://api.example.com  ', 'https://api.example.com'],
  ])('reads %s as %s', (url, want) => {
    expect(apiOrigin(url)).toBe(want)
  })

  it.each([undefined, '', '   ', 'not a url', 'javascript:alert(1)', 'data:text/html,hi'])('gives nothing for %s', (url) => {
    expect(apiOrigin(url)).toBeNull()
  })
})

describe('contentSecurityPolicy', () => {
  it('lets the page talk only to itself and to the API', () => {
    const policy = contentSecurityPolicy('https://shadow-pager-api.onrender.com', [])
    expect(directive(policy, 'default-src')).toBe("default-src 'self'")
    expect(directive(policy, 'connect-src')).toBe("connect-src 'self' https://shadow-pager-api.onrender.com")
    expect(directive(policy, 'object-src')).toBe("object-src 'none'")
  })

  it('lets the page talk only to itself when the API shares its address', () => {
    expect(directive(contentSecurityPolicy(null, []), 'connect-src')).toBe("connect-src 'self'")
  })

  it('never opens a door wider than it has to', () => {
    const policy = contentSecurityPolicy('https://api.example.com', ["'sha256-abc='"])
    expect(policy).not.toContain('*')
    expect(policy).not.toContain('unsafe-eval')
    expect(directive(policy, 'script-src')).toBe("script-src 'self' 'sha256-abc='")
    // Inline styles are allowed on attributes only, never as whole style sheets.
    expect(directive(policy, 'style-src')).toBe("style-src 'self'")
    expect(directive(policy, 'style-src-attr')).toBe("style-src-attr 'unsafe-inline'")
  })
})

describe('inline scripts', () => {
  it('finds scripts written into the page and skips the ones loaded from a file', () => {
    const html = '<script>one()</script><script type="module" src="/src/main.tsx"></script><script>\n two()\n</script><script></script>'
    expect(inlineScripts(html)).toEqual(['one()', '\n two()\n'])
  })

  it('hashes a script the way a browser does', async () => {
    // This is the well known SHA-256 of the text "alert(1)".
    expect(await scriptHash('alert(1)')).toBe("'sha256-bhHHL3z2vDgxUt0W3dWQOrprscmda2Y5pLsLg4GF+pI='")
  })
})

describe('the real page', () => {
  it('gets a policy that allows its own theme script and the API, and nothing else from outside', async () => {
    const scripts = inlineScripts(indexHtml)
    expect(scripts).toHaveLength(1)
    expect(scripts[0]).toContain('shadow-pager.theme')

    const html = await applyPolicy(indexHtml, 'https://shadow-pager-api.onrender.com/')
    const policy = /<meta http-equiv="Content-Security-Policy" content="([^"]+)"/.exec(html)?.[1] ?? ''

    expect(directive(policy, 'script-src')).toBe(`script-src 'self' ${await scriptHash(scripts[0] ?? '')}`)
    expect(directive(policy, 'connect-src')).toBe("connect-src 'self' https://shadow-pager-api.onrender.com")
    // The policy comes before the first script, so it is already in force when that script runs.
    expect(html.indexOf('Content-Security-Policy')).toBeLessThan(html.indexOf('<script'))
    expect(html.indexOf('Content-Security-Policy')).toBeGreaterThan(html.indexOf('<head>'))
  })

  it('writes the policy into the head exactly once', () => {
    const html = withPolicy('<html><head><title>x</title></head></html>', "default-src 'self'")
    expect(html.match(/Content-Security-Policy/g)).toHaveLength(1)
    expect(html).toContain(`content="default-src 'self'"`)
  })
})

describe('vercel.json', () => {
  const config = JSON.parse(vercelJson) as VercelConfig
  const everywhere = config.headers?.find((rule) => rule.source === '/(.*)')
  const header = (key: string) => everywhere?.headers.find((h) => h.key === key)?.value

  it('sends the security headers with every file', () => {
    expect(header('X-Content-Type-Options')).toBe('nosniff')
    expect(header('Referrer-Policy')).toBe('strict-origin-when-cross-origin')
    expect(header('X-Frame-Options')).toBeUndefined()
  })

  it('tells browsers to insist on HTTPS for one year, on every subdomain too', () => {
    // One year is 31,536,000 seconds.
    expect(header('Strict-Transport-Security')).toBe('max-age=31536000; includeSubDomains')
  })

  it('blocks framing in a header, because a policy inside the page is not allowed to', () => {
    const policy = header('Content-Security-Policy') ?? ''
    expect(directive(policy, 'frame-ancestors')).toBe("frame-ancestors *")
    expect(directive(policy, 'object-src')).toBe("object-src 'none'")
    // The header must not narrow what the page may fetch, or it would block the API that only the build knows.
    expect(directive(policy, 'default-src')).toBeUndefined()
    expect(directive(policy, 'connect-src')).toBeUndefined()
  })
})
