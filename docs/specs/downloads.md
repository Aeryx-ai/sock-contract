# Downloads

Pass 1. Contexts: none of the three; a new `analytics` schema of aggregate facts. Decision: sock-server ADR 53.

What this is: the hub counts fetches of release files and keeps the counts by asset and UTC day, forever. The website's manifests send every download through the hub's redirect, so the Download page, `install.sh` and any other reader of a manifest are counted; a raw bucket link is not.

## Ubiquitous language

| Term | Meaning | Stored or derived |
|---|---|---|
| Download asset | One release file in the bucket, named by its key `releases/<platform>/<version>/<file>`, the three parts split out | Stored (`analytics.download_assets`) |
| Daily downloads | How many times an asset was fetched through the redirect on one UTC day | Stored (`analytics.daily_downloads`) |
| Counted download | `GET /download/<key>` on the hub: the count, then a `302` to the bucket | Not stored beyond the count |

Not a term here: a request, a visitor, an address. The hub keeps none of them.

## Domain model

### Analytics (new, not a bounded context)

Two facts, no behavior beyond `Record` and `Sum`. `DownloadAsset` is a value object: a key matching `^releases/[a-z0-9-]+/[A-Za-z0-9._+-]+/[A-Za-z0-9._+-]+$` (the parts as `publish-release.sh` admits them), at most 512 bytes, with its platform, version and file. Anything else, the manifests and appcasts included, is not an asset. `DailyDownloads.Record(asset, day)` adds one. `Sum(by, since)` groups by asset, platform, version or day.

## Configuration

| Variable | Meaning |
|---|---|
| `SOCK_DOWNLOADS_URL` | The bucket's public origin the redirect sends to: https, no path, userinfo, query or fragment. Unset: `https://downloads.joinsock.com`. The route is on wherever sockd runs |

Website: `publish-release.sh` writes each manifest's `url` as `https://api.joinsock.com/download/<key>` (`DOWNLOAD_VIA` overrides the prefix); the Download page accepts a manifest `url` only on the API origin's `/download/`; `install.sh` follows the redirect.

## Caller classes

Anonymous gains the route; it reaches no account and no identity. The limit is the route's own.

## API

### Hub route (new, [api](api.md), Operational)

| Route | Request | Response |
|---|---|---|
| `GET /download/<key>` | `<key>` a download asset's key, at most 512 bytes. Any other path under `/download/` is `404` with nothing echoed. 60 a minute per client network with a burst of 20, in each replica's memory: `429` with `Retry-After` past it | `Record(asset, today UTC)`, then `302` to `<SOCK_DOWNLOADS_URL>/<key>` with `Cache-Control: no-store` and `Referrer-Policy: no-referrer`. A failed record is logged and the redirect still goes: the count never stands between a person and the file |

No RPC. `sockd downloads [--by asset|platform|version|day] [--days N]` prints `Sum`. An operator RPC and a console page are a later pass.

## Domain events

None. A count is its own record.

## Data layer

```sql
CREATE SCHEMA analytics;

CREATE TABLE analytics.download_assets (
    key        text PRIMARY KEY CHECK (key ~ '^releases/[a-z0-9-]+/[A-Za-z0-9._+-]+/[A-Za-z0-9._+-]+$'),
        -- the bucket key: the natural key
    platform   text NOT NULL CHECK (platform ~ '^[a-z0-9-]+$'),
    version    text NOT NULL CHECK (version ~ '^[A-Za-z0-9._+-]+$'),
    file       text NOT NULL CHECK (file ~ '^[A-Za-z0-9._+-]+$'),
        -- the key's parts, split once so a report by platform or version is a
        -- GROUP BY; the key is their join, the one recorded derived column
    first_seen date NOT NULL DEFAULT (now() AT TIME ZONE 'UTC')::date
);
CREATE INDEX download_assets_platform_idx ON analytics.download_assets (platform, version);

CREATE TABLE analytics.daily_downloads (
    asset_key text   NOT NULL REFERENCES analytics.download_assets (key) ON DELETE RESTRICT,
    day       date   NOT NULL,                  -- UTC
    count     bigint NOT NULL CHECK (count > 0),
    PRIMARY KEY (asset_key, day)                -- the redirect's upsert
);
CREATE INDEX daily_downloads_day_idx ON analytics.daily_downloads (day);
```

No retention: the aggregate is the point. Finer facts later (country, referrer) are new fact tables here, not columns on these.

## Threats

| Threat | Mitigation |
|---|---|
| Inflating a count | 60 a minute per client network per replica; one address needs many networks to move a number |
| An open redirect | Only a key that parses as a release file is redirected, to one configured origin; the `302` carries no request text |
| Counting a person | Nothing but the asset and the day is written |
| The count blocking downloads | A failed write is logged; the redirect goes regardless |

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| `DailyDownloads.Record` | `GET /download/<key>` | none | `analytics.download_assets` insert on first sight, `analytics.daily_downloads` upsert |
| `Sum` | `sockd downloads` | none | read |
