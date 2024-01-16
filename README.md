# redirector

A simple HTTP server to redirect traffic from one place to another.

## Configuration

Add a configuration file at `config.json` with content like:

```json
{
 "listen_address": ":8080",
 "default_response": {
  "code": 421,
  "body": "421 Misdirected Request\n\nTarget URI does not match an origin for which the server has been configured.\n",
  "headers": {
   "Connection":   "close",
   "Content-Type": "text/plain"
  },
  "log_hits": true
 },
 "domains": {
  "example.com": {
   "match_subdomains": true,
   "rewrites": [
    {
     "regexp": "^(.*)$",
     "replacement": "https://www.example.com$1",
     "code": 301,
     "log_hits": true
    }
   ]
  }
 }
}
```

For `default_response`, using a code of `0` will result in the connection being immediately closed if possible. If that is not possible at runtime, the headers and body will be used but with an HTTP status code of 500.

Each domain may also define a `default_response` key which matches if no `rewrites` match.

All regular expressions use [re2](https://github.com/google/re2/wiki/Syntax) syntax.

For a given rewrite, `replacement` may include variables like `$1` where the number will be replaced with the corresponding matched sub-pattern with that index. Replacing with named sub-patterns is not currently supported, and attempting to use a non-numeric variable will cause validation of configuration to fail. To insert a literal `$`, use `$$`.

Rewrites are applied in order, and only the first matching rewrite is applied. If there are duplicate domains, only the first matching domain is used.

If `match_subdomains` is true, all subdomains (including nested subdomains e.g. `a.b.example.com` for `example.com`) will be matched. It is an error to set `match_subdomains` to true if a matching subdomain is also elsewhere defined (e.g. you cannot do `{"example.com": { "match_subdomains": true }, "www.example.com": {}`).

Domains must be lowercase ASCII (i.e. in punycode if required). Domains may include a port after a colon (e.g. `example.com:8080`), but will be matched against the `Host` header directly, so use of `:80` or `:443` is not recommended as most clients do not include that in the `Host` header when using HTTP(S) on those ports.
