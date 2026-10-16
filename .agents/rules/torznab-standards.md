# Torznab & Prowlarr/Arr Integration Standards

When modifying Torznab endpoints, RSS feeds, or frontend routing in OmniDex, adhere to the following architectural patterns:

## 1. Torznab Feeds & XML Compliance
- **Content-Type**: Always serve Torznab responses with `application/rss+xml; charset=utf-8` or `text/xml; charset=utf-8`. Never return JSON or unformatted error strings on Torznab routes (`/api`, `/torznab/api`).
- **Capabilities (`t=caps`)**: Must return standard `<caps>` XML defining `<server>`, `<searching>`, and `<categories>`.

## 2. Torrent Links & Download Proxies
- **HTTP Enclosures**: In `<item>` RSS elements, set both `<link>` and `<enclosure url="...">` to an HTTP download URL (e.g., `${baseURL}/download/${infoHash}`) rather than a raw `magnet:` URI.
- **Dual Handling on `/download/<infohash>`**:
  - For automation clients (Sonarr, Radarr, Lidarr, Prowlarr, qBittorrent, curl/wget), immediately return HTTP 302 redirecting to the `magnet:` URI.
  - For browser user-agents, return an HTML launcher page with an immediate JS magnet launch (`window.location.href`), a visible fallback button, and a copy magnet button to prevent `about:blank` tabs.
- **Torznab Magnet Attribute**: Always supply `<torznab:attr name="magneturl" value="${magnetURI}"/>` in the `<item>` to allow direct attribute-based extraction.

## 3. SPA Deep-Linking for `<comments>`
- Torznab items include `<comments>${baseURL}/#${infoHash}</comments>`.
- The frontend router must parse `window.location.hash` on `DOMContentLoaded` and `hashchange`. If a valid 32-to-40 character hexadecimal infohash is detected, it must automatically fetch the torrent's metadata and display the details modal.
