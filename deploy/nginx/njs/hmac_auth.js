// Edge HMAC token validator (njs). Mirrors internal/token/token.go:
//
//   prefix = "/hls/<videoId>/"            (first three path segments)
//   data   = prefix + "\n" + <exp>        (exp is the raw ?exp= string)
//   sig    = base64url( HMAC_SHA256(secret, data) )   // unpadded, URL-safe
//
// A single token authorizes the whole prefix, so the master playlist, variant
// playlists, init segments and every media segment validate with the same
// ?exp=&token= query. Validation runs in-process before the cache lookup.
//
// Two things beyond a plain check:
//  1. Preview/sidecar assets (poster, sprite, thumbnails, captions, chapters)
//     are NOT the protected video stream — they are served without a token (the
//     unguessable video id in the path is their protection). This also avoids
//     the relative-URL token-drop problem for sprite images referenced from a
//     VTT. Only the stream files (.m3u8/.m4s/.mp4) require a token.
//  2. KEY ROTATION: both TOKEN_SECRET and TOKEN_SECRET_PREVIOUS are accepted,
//     so a secret can be rotated without invalidating outstanding tokens.

import crypto from 'crypto';

function b64url(s) {
    return s.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function sign(secret, data) {
    return b64url(crypto.createHmac('sha256', secret).update(data).digest('base64'));
}

function constantEq(a, b) {
    if (a.length !== b.length) return false;
    var diff = 0;
    for (var i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
    return diff === 0;
}

// validate is wired via `js_set $token_valid hmac.validate;`
function validate(r) {
    var uri = r.uri;

    // Preview/sidecar assets: allowed without a token.
    if (/\.(jpe?g|png|webp|vtt)$/i.test(uri)) {
        return '1';
    }

    var token = r.args.token;
    var expStr = r.args.exp;
    if (!token || !expStr) {
        return '0';
    }
    var exp = Number(expStr);
    if (!Number.isFinite(exp) || exp < Math.floor(Date.now() / 1000)) {
        return '0';
    }

    // Stream files live under /hls/<id>/; original files (Early-Play / download)
    // live under /raw/<id>/. A token authorizes the whole <prefix>.
    var m = uri.match(/^(\/(?:hls|raw)\/[^\/]+\/)/);
    if (m === null) {
        return '0';
    }
    var data = m[1] + '\n' + expStr;

    var secrets = [process.env.TOKEN_SECRET || ''];
    if (process.env.TOKEN_SECRET_PREVIOUS) {
        secrets.push(process.env.TOKEN_SECRET_PREVIOUS);
    }
    for (var i = 0; i < secrets.length; i++) {
        if (constantEq(sign(secrets[i], data), token)) {
            return '1';
        }
    }
    return '0';
}

export default { validate };
