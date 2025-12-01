// ==============================================================================
// host-resolver.js — Dynamic host-to-prefix resolution for the NGINX S3 gateway
//
// Resolves incoming Host headers to S3 published prefixes by querying
// DynamoDB (SigV4-signed here, sent via the internal /_ddb_getitem proxy).  Uses an in-memory
// js_shared_dict cache to amortise the DynamoDB lookup.
//
// The module exports resolveAndRedirect(r) for use as a js_content handler.
// It replaces the upstream s3gateway.redirectToS3 entry point: on success
// it delegates to the upstream handler via import; on failure it returns
// a clean 404.
//
// Design:   docs/design-gateway-host-to-prefix-resolution.md
// Issue:    #79 — Implement dynamic host-to-prefix resolution in the gateway
// ==============================================================================

import s3gateway from "../include/s3gateway.js";

const mod_crypto = require('crypto');
const mod_fs = require('fs');

// ==============================================================================
// Configuration — environment variables
// ==============================================================================

const TABLE_NAME = process.env['SITE_METADATA_TABLE_NAME'];
const AWS_REGION = process.env['AWS_REGION'] || process.env['S3_REGION'] || '';
const DEBUG_ENABLED = process.env['DEBUG'] ? true : false;
const CACHE_TTL_SECONDS = parseInt(process.env['RESOLVE_CACHE_TTL_SECONDS'] || '5', 10);
const NEGATIVE_CACHE_TTL_SECONDS = CACHE_TTL_SECONDS * 6;  // 30 s default

// ==============================================================================
// DynamoDB constants
// ==============================================================================

const DDB_HOST = "dynamodb." + AWS_REGION + ".amazonaws.com";
const DDB_URI = "/";
const DDB_SERVICE = "dynamodb";
const DDB_TARGET_HEADER = "DynamoDB_20120810.GetItem";
const DDB_SIGNED_HEADERS_BASE = "host;x-amz-content-sha256;x-amz-date;x-amz-target";

// ==============================================================================
// Cache (js_shared_dict)
// ==============================================================================

var _cache;
try {
    _cache = require('js_shared_dict').get('resolve_cache');
} catch (_e) {
    // No cache zone configured — all requests go to DynamoDB.
    _cache = null;
}

function cacheGet(key) {
    if (!_cache) { return undefined; }
    try {
        var raw = _cache.get(key);
        if (!raw) { return undefined; }
        var entry = JSON.parse(raw);
        if (Date.now() > entry.e) {
            _cache.delete(key);
            return undefined;
        }
        return entry;
    } catch (_e) {
        return undefined;
    }
}

function cacheSet(key, entry) {
    if (!_cache) { return; }
    try {
        _cache.set(key, JSON.stringify(entry));
    } catch (_e) {
        // Zone full or type mismatch — carry on.
    }
}

// ==============================================================================
// Credential reading
//
// On ECS Fargate the upstream docker-entrypoint.sh's 40-refresh-aws-credentials
// writes credentials to /tmp/credentials.json.  We prefer the environment
// variables (injected by the same script) when available.
// ==============================================================================

function readCredentials() {
    var keyId = process.env['AWS_ACCESS_KEY_ID'];
    var secret = process.env['AWS_SECRET_ACCESS_KEY'];
    if (keyId && secret) {
        return {
            accessKeyId: keyId,
            secretAccessKey: secret,
            sessionToken: process.env['AWS_SESSION_TOKEN'] || null,
        };
    }
    // Fall back to the file cache written by the upstream credential refresh.
    try {
        var raw = mod_fs.readFileSync('/tmp/credentials.json');
        return JSON.parse(raw);
    } catch (_e) {
        return null;
    }
}

// ==============================================================================
// DynamoDB SigV4 signing
// ==============================================================================

function _getDateString(date) {
    var y = String(date.getUTCFullYear());
    var m = String(date.getUTCMonth() + 1).padStart(2, '0');
    var d = String(date.getUTCDate()).padStart(2, '0');
    return y + m + d;
}

function _getAmzDatetime(date) {
    var ds = _getDateString(date);
    var h = String(date.getUTCHours()).padStart(2, '0');
    var mi = String(date.getUTCMinutes()).padStart(2, '0');
    var s = String(date.getUTCSeconds()).padStart(2, '0');
    return ds + 'T' + h + mi + s + 'Z';
}

function _buildCanonicalRequest(method, uri, host, amzDate, payloadHash, sessionToken, targetHeader) {
    var canonicalHeaders = "host:" + host + "\n" +
        "x-amz-content-sha256:" + payloadHash + "\n" +
        "x-amz-date:" + amzDate + "\n" +
        "x-amz-target:" + targetHeader + "\n";
    var signedHeaders = DDB_SIGNED_HEADERS_BASE;
    if (sessionToken) {
        canonicalHeaders += "x-amz-security-token:" + sessionToken + "\n";
        signedHeaders += ";x-amz-security-token";
    }
    return method + "\n" +
        uri + "\n" +
        "\n" +                     // Empty query string
        canonicalHeaders + "\n" +
        signedHeaders + "\n" +
        payloadHash;
}

function _buildSigningKey(secretKey, dateStr, region, service) {
    var kDate = mod_crypto.createHmac('sha256', 'AWS4' + secretKey)
        .update(dateStr).digest();
    var kRegion = mod_crypto.createHmac('sha256', kDate)
        .update(region).digest();
    var kService = mod_crypto.createHmac('sha256', kRegion)
        .update(service).digest();
    return mod_crypto.createHmac('sha256', kService)
        .update('aws4_request').digest();
}

function signDynamoDBRequest(credentials, requestBody) {
    var now = new Date();
    var dateStr = _getDateString(now);
    var amzDate = _getAmzDatetime(now);
    var payloadHash = mod_crypto.createHash('sha256')
        .update(requestBody, 'utf8').digest('hex');

    var canonicalRequest = _buildCanonicalRequest(
        "POST", DDB_URI, DDB_HOST, amzDate, payloadHash,
        credentials.sessionToken, DDB_TARGET_HEADER
    );
    var canonicalHash = mod_crypto.createHash('sha256')
        .update(canonicalRequest).digest('hex');

    var credentialScope = dateStr + "/" + AWS_REGION + "/" + DDB_SERVICE + "/aws4_request";
    var stringToSign = "AWS4-HMAC-SHA256\n" +
        amzDate + "\n" +
        credentialScope + "\n" +
        canonicalHash;

    var kSigning = _buildSigningKey(
        credentials.secretAccessKey, dateStr, AWS_REGION, DDB_SERVICE
    );
    var signature = mod_crypto.createHmac('sha256', kSigning)
        .update(stringToSign).digest('hex');

    var signedHeaders = DDB_SIGNED_HEADERS_BASE;
    if (credentials.sessionToken) {
        signedHeaders += ";x-amz-security-token";
    }

    var authHeader = "AWS4-HMAC-SHA256 Credential=" +
        credentials.accessKeyId + "/" + credentialScope +
        ",SignedHeaders=" + signedHeaders +
        ",Signature=" + signature;

    return {
        authorization: authHeader,
        amzDate: amzDate,
        amzTarget: DDB_TARGET_HEADER,
        payloadHash: payloadHash,
        sessionToken: credentials.sessionToken,
    };
}

// ==============================================================================
// DynamoDB response unmarshalling
// ==============================================================================

function _unmarshalAttribute(av) {
    if (typeof av !== 'object' || av === null) { return av; }
    if ('S' in av)  { return av.S; }
    if ('N' in av)  { return Number(av.N); }
    if ('BOOL' in av) { return av.BOOL; }
    if ('NULL' in av) { return null; }
    if ('L' in av)  { return av.L.map(_unmarshalAttribute); }
    if ('M' in av)  {
        var out = {};
        for (var k in av.M) { out[k] = _unmarshalAttribute(av.M[k]); }
        return out;
    }
    // Binary, SS, NS, BS — not expected in our items; return raw.
    return av;
}

function unmarshalItem(item) {
    var result = {};
    for (var key in item) {
        if (Object.prototype.hasOwnProperty.call(item, key)) {
            result[key] = _unmarshalAttribute(item[key]);
        }
    }
    return result;
}

// ==============================================================================
// Hostname normalisation
// ==============================================================================

function normaliseHost(hostname) {
    if (!hostname) { return ''; }
    // Strip port suffix if present (e.g. "example.com:443").
    var colonIdx = hostname.lastIndexOf(':');
    if (colonIdx !== -1) {
        // Guard against IPv6 addresses: port has no ']' after the colon.
        var afterColon = hostname.substring(colonIdx + 1);
        if (/^\d+$/.test(afterColon)) {
            hostname = hostname.substring(0, colonIdx);
        }
    }
    return hostname.toLowerCase();
}

// ==============================================================================
// Core resolution — DynamoDB GetItem on HOST#<hostname> / MAPPING
// ==============================================================================

function debugLog(r, msg) {
    if (DEBUG_ENABLED && r && typeof r.log === 'function') {
        r.log('[host-resolver] ' + msg);
    }
}

/**
 * Resolve a hostname to an S3 published prefix.
 *
 * Returns { prefix } on success, { error } on failure.
 * Caches both positive and negative results in js_shared_dict.
 */
async function resolve(r, hostname) {
    var norm = normaliseHost(hostname);
    if (!norm) {
        return { error: 'host_not_found' };
    }

    // --- Cache lookup ----------------------------------------------------------
    var cached = cacheGet(norm);
    if (cached) {
        if (cached.p) {
            debugLog(r, 'cache hit (positive): ' + norm + ' -> ' + cached.p);
            return { prefix: cached.p };
        }
        if (cached.err) {
            debugLog(r, 'cache hit (negative): ' + norm + ' -> ' + cached.err);
            return { error: cached.err };
        }
    }

    // --- Credentials -----------------------------------------------------------
    var credentials = readCredentials();
    if (!credentials) {
        debugLog(r, 'no credentials available for DynamoDB call');
        return { error: 'dynamodb_error' };
    }

    // --- Build DynamoDB GetItem request ----------------------------------------
    var requestBody = JSON.stringify({
        TableName: TABLE_NAME,
        Key: {
            pk:  { S: 'HOST#' + norm },
            sk:  { S: 'MAPPING' },
        },
        ConsistentRead: false,
    });

    debugLog(r, 'DynamoDB GetItem for HOST#' + norm);

    // --- Sign and send ---------------------------------------------------------
    var sig = signDynamoDBRequest(credentials, requestBody);

    // ngx.fetch in this image sends plain HTTP to https:// URLs, so the signed
    // request goes through the internal /_ddb_getitem proxy location instead;
    // the signed headers travel in variables shared with the subrequest.
    r.variables.ddb_amz_date = sig.amzDate;
    r.variables.ddb_payload_hash = sig.payloadHash;
    r.variables.ddb_authorization = sig.authorization;
    r.variables.ddb_session_token = sig.sessionToken || '';

    var reply;
    try {
        reply = await r.subrequest('/_ddb_getitem', { method: 'POST', body: requestBody });
    } catch (e) {
        debugLog(r, 'DynamoDB subrequest failed: ' + String(e));
        return { error: 'dynamodb_error' };
    }

    if (reply.status !== 200) {
        debugLog(r, 'DynamoDB returned HTTP ' + reply.status + ' ' + reply.responseText);
        return { error: 'dynamodb_error' };
    }

    var body;
    try {
        body = JSON.parse(reply.responseText);
    } catch (e) {
        debugLog(r, 'DynamoDB response parse error: ' + String(e));
        return { error: 'dynamodb_error' };
    }

    // --- No item → host not found ----------------------------------------------
    if (!body.Item) {
        debugLog(r, 'host not found: ' + norm);
        cacheSet(norm, {
            err: 'host_not_found',
            e: Date.now() + NEGATIVE_CACHE_TTL_SECONDS * 1000,
        });
        return { error: 'host_not_found' };
    }

    var item = unmarshalItem(body.Item);

    // --- Disabled checks (denormalised flags) ----------------------------------
    if (item.userDisabled === true) {
        debugLog(r, 'user disabled for host ' + norm);
        cacheSet(norm, {
            err: 'user_disabled',
            e: Date.now() + CACHE_TTL_SECONDS * 1000,
        });
        return { error: 'user_disabled' };
    }
    if (item.siteDisabled === true) {
        debugLog(r, 'site disabled for host ' + norm);
        cacheSet(norm, {
            err: 'site_disabled',
            e: Date.now() + CACHE_TTL_SECONDS * 1000,
        });
        return { error: 'site_disabled' };
    }

    // --- No active version -----------------------------------------------------
    if (!item.s3PublishedPrefix || !item.versionId) {
        debugLog(r, 'no active version for host ' + norm);
        cacheSet(norm, {
            err: 'no_active_version',
            e: Date.now() + CACHE_TTL_SECONDS * 1000,
        });
        return { error: 'no_active_version' };
    }

    // --- Validate prefix (defence-in-depth) ------------------------------------
    if (item.s3PublishedPrefix.indexOf('published/') !== 0) {
        debugLog(r, 'invalid prefix for host ' + norm + ': ' + item.s3PublishedPrefix);
        cacheSet(norm, {
            err: 'invalid_prefix',
            e: Date.now() + CACHE_TTL_SECONDS * 1000,
        });
        return { error: 'invalid_prefix' };
    }

    // --- Success ---------------------------------------------------------------
    var prefix = item.s3PublishedPrefix;
    debugLog(r, 'resolved ' + norm + ' -> ' + prefix);
    cacheSet(norm, {
        p: prefix,
        e: Date.now() + CACHE_TTL_SECONDS * 1000,
    });
    return { prefix: prefix };
}

// ==============================================================================
// Public API — js_content handler
//
// Replaces the upstream s3gateway.redirectToS3 in location /.  Resolves the
// Host header, prefixes r.variables.uri_path with it on success, or
// redirects to @gateway_error on failure.
// ==============================================================================

async function resolveAndRedirect(r) {
    var hostname = r.headersIn['Host'] || '';

    var result = await resolve(r, hostname);

    if (result.error) {
        // Log at detail level matching the error category (per design §4.3).
        var logLevel = 'warn';
        if (result.error === 'host_not_found') {
            logLevel = 'warn';
        } else if (result.error === 'user_disabled' || result.error === 'site_disabled' || result.error === 'no_active_version') {
            logLevel = 'info';
        } else {
            logLevel = 'error';
        }
        debugLog(r, 'resolution failed (' + logLevel + '): ' + result.error + ' for ' + hostname);

        r.variables.s3_resolve_error = result.error;
        r.internalRedirect('@gateway_error');
        return;
    }

    // Upstream s3uri/s3auth build the S3 key and signature from $uri_path, so
    // prefix it here: "published/.../v1/" + "/about/" -> "/published/.../v1/about/".
    r.variables.uri_path = '/' + result.prefix.replace(/\/+$/, '') + r.variables.uri_full_path;

    // Delegate to the upstream content handler.
    s3gateway.redirectToS3(r);
}

export default { resolveAndRedirect, resolve };
