// Offline view: board pages and their fragments are fetched from the
// network first; the last good answer is kept and served when offline.
// Logout clears it (Clear-Site-Data).
"use strict";

var CACHE = "andon-last";
var KEEP = [/^\/$/, /^\/boards\/\d+$/, /^\/widget-fragments\//, /^\/static\//, /^\/icons\//, /^\/theme\//];

function keepable(url) {
  if (url.origin !== self.location.origin || url.searchParams.has("edit") || url.searchParams.has("layout")) {
    return false;
  }
  return KEEP.some(function (re) { return re.test(url.pathname); });
}

self.addEventListener("install", function () { self.skipWaiting(); });
self.addEventListener("activate", function (e) { e.waitUntil(self.clients.claim()); });

// versioned files (?v=) never change: served from the cache without a
// network round trip, and stored once instead of on every page load.
function versioned(url) {
  return /^\/(static|theme)\//.test(url.pathname) && url.searchParams.has("v");
}

// fetchKeep asks the network and keeps a good answer.
function fetchKeep(req) {
  return fetch(req).then(function (res) {
    if (res.ok && res.type === "basic" && !res.redirected) {
      var copy = res.clone();
      caches.open(CACHE).then(function (c) { c.put(req, copy); });
    }
    return res;
  });
}

self.addEventListener("fetch", function (e) {
  var req = e.request;
  var url = new URL(req.url);
  if (req.method !== "GET" || !keepable(url)) {
    return;
  }
  if (versioned(url)) {
    e.respondWith(caches.match(req).then(function (hit) { return hit || fetchKeep(req); }));
    return;
  }
  e.respondWith(fetchKeep(req).catch(function () {
    return caches.match(req).then(function (hit) { return hit || Response.error(); });
  }));
});
