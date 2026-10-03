/* Kante map: a vector map (Protomaps basemap, drawn by MapLibre GL) in Kante's colours, with a
 * route and pins. The page only marks the frame; this script loads the map libraries on first use.
 *
 *   <div class="map-frame" data-map='{"route":[[9.93,53.55],…],"marks":[{"lon":9.9,"lat":53.5,"title":"…","state":"bad"}]}'
 *        data-map-source="https://example.org/germany.pmtiles"></div>
 *   <script src="…/kante-map.js"></script>   then Kante.map.mount(root) for frames added later
 *
 * data-map-source is a .pmtiles file (read in ranges) or a TileJSON URL (e.g. the Protomaps API
 * with its key). Every colour comes from Kante's tokens, read from the page, so the map follows
 * the theme (dark, Leinen). The libraries sit in map/ next to this file (BSD-3, see the licences).
 */
(function () {
  'use strict';
  var base = (document.currentScript && document.currentScript.src || '').replace(/[^/]*$/, '') + 'map/';
  var ASSETS = 'https://protomaps.github.io/basemaps-assets/';
  var SOURCE = 'protomaps';
  var NEAR_ZOOM = 13;     // one point, or points close together
  var PADDING = 36;       // px between the points and the frame's edge
  var libs = null;

  // load fetches MapLibre (an ES module with its worker next to it), pmtiles and basemaps once.
  function load() {
    if (libs) { return libs; }
    var css = document.createElement('link');
    css.rel = 'stylesheet';
    css.href = base + 'maplibre-gl.css';
    document.head.appendChild(css);
    var script = function (name) {
      return new Promise(function (ok, fail) {
        var s = document.createElement('script');
        s.src = base + name;
        s.onload = ok;
        s.onerror = fail;
        document.head.appendChild(s);
      });
    };
    libs = Promise.all([import(base + 'maplibre-gl.mjs'), script('pmtiles.js'), script('basemaps.js')]).then(function (r) {
      var maplibre = r[0];
      maplibre.addProtocol('pmtiles', new window.pmtiles.Protocol().tile);
      return maplibre;
    });
    return libs;
  }

  // colour resolves a token (also an alias like var(--focus)) to rgb().
  function colour(token) {
    var probe = document.createElement('span');
    probe.style.color = 'var(' + token + ')';
    probe.style.display = 'none';
    document.body.appendChild(probe);
    var c = getComputedStyle(probe).color;
    probe.remove();
    return c;
  }

  // mix blends two rgb() colours: share of a.
  function mix(a, b, share) {
    var pa = a.match(/[\d.]+/g).map(Number), pb = b.match(/[\d.]+/g).map(Number);
    var c = [0, 1, 2].map(function (i) { return Math.round(pa[i] * share + pb[i] * (1 - share)); });
    return 'rgb(' + c.join(',') + ')';
  }

  // light tells the Leinen theme from the dark one by the ground's brightness.
  function light(ground) {
    var p = ground.match(/[\d.]+/g).map(Number);
    return (p[0] + p[1] + p[2]) / 3 > 127;
  }

  // flavor is Protomaps' flavor with Kante's roles: ground and land from the backgrounds, water
  // from blue, green areas from aqua, roads from the raised backgrounds, labels in the text greys.
  function flavor() {
    var t = {};
    ['--bg-void', '--bg-hard', '--bg0', '--bg1', '--bg2', '--fg1', '--fg2', '--fg3', '--blue', '--blue-n',
      '--aqua-n', '--yellow-n', '--orange-n'].forEach(function (k) { t[k] = colour(k); });
    var isLight = light(t['--bg0']);
    var f = Object.assign({}, window.basemaps.namedFlavor(isLight ? 'light' : 'dark'));
    var green = mix(t['--aqua-n'], t['--bg0'], 0.18);
    var water = mix(t['--blue-n'], t['--bg-hard'], 0.4);
    var road = t['--bg1'], major = t['--bg2'], casing = t['--bg-hard'];
    var highway = mix(t['--yellow-n'], t['--bg2'], 0.35);
    Object.assign(f, {
      background: t['--bg-hard'], earth: t['--bg0'], water: water,
      park_a: green, park_b: green, wood_a: green, wood_b: green, scrub_a: green, scrub_b: green, zoo: green,
      hospital: t['--bg0'], industrial: t['--bg0'], school: t['--bg0'], military: t['--bg0'],
      pedestrian: road, sand: t['--bg1'], beach: t['--bg1'], aerodrome: t['--bg1'], runway: t['--bg2'],
      glacier: t['--bg1'], pier: t['--bg1'], buildings: t['--bg1'],
      other: road, minor_service: road, minor_a: road, minor_b: road, link: major, major: major, highway: highway,
      minor_service_casing: casing, minor_casing: casing, link_casing: casing, major_casing_late: casing,
      major_casing_early: casing, highway_casing_late: casing, highway_casing_early: casing,
      railway: t['--bg2'], boundaries: t['--fg3'],
      roads_label_minor: t['--fg3'], roads_label_minor_halo: casing, roads_label_major: t['--fg3'], roads_label_major_halo: casing,
      ocean_label: t['--blue'], subplace_label: t['--fg3'], subplace_label_halo: casing,
      city_label: t['--fg1'], city_label_halo: casing, state_label: t['--fg3'], state_label_halo: casing,
      country_label: t['--fg2'], address_label: t['--fg3'], address_label_halo: casing
    });
    return { flavor: f, light: isLight };
  }

  // style builds the MapLibre style for a source URL.
  function style(source, lang) {
    var fl = flavor();
    var src = /\.pmtiles(\?|$)/.test(source) ? { type: 'vector', url: 'pmtiles://' + source } : { type: 'vector', url: source };
    src.attribution = '<a href="https://openstreetmap.org/copyright" target="_blank" rel="noopener">© OpenStreetMap</a> · ' +
      '<a href="https://protomaps.com" target="_blank" rel="noopener">Protomaps</a>';
    var sources = {};
    sources[SOURCE] = src;
    return {
      version: 8,
      glyphs: ASSETS + 'fonts/{fontstack}/{range}.pbf',
      sprite: ASSETS + 'sprites/v4/' + (fl.light ? 'light' : 'dark'),
      sources: sources,
      layers: window.basemaps.layers(SOURCE, fl.flavor, { lang: lang })
    };
  }

  // draw puts the route and the pins on a map and frames them.
  function draw(maplibre, map, el, data) {
    var route = data.route || [], marks = data.marks || [];
    if (route.length > 1) {
      map.addSource('kante-route', { type: 'geojson', data: { type: 'Feature', geometry: { type: 'LineString', coordinates: route } } });
      map.addLayer({ id: 'kante-route', type: 'line', source: 'kante-route',
        layout: { 'line-join': 'round', 'line-cap': 'round' }, paint: { 'line-color': colour('--map-route'), 'line-width': 3 } });
    }
    marks.forEach(function (m) {
      var pin = document.createElement('span');
      pin.className = 'map-pin';
      if (m.state) { pin.setAttribute('data-state', m.state); }
      if (m.title) { pin.title = m.title; }
      new maplibre.Marker({ element: pin }).setLngLat([m.lon, m.lat]).addTo(map);
    });
    var all = route.concat(marks.map(function (m) { return [m.lon, m.lat]; }));
    if (!all.length) { return; }
    var bounds = all.reduce(function (b, p) { return b.extend(p); }, new maplibre.LngLatBounds(all[0], all[0]));
    map.fitBounds(bounds, { padding: PADDING, maxZoom: NEAR_ZOOM, duration: 0 });
  }

  // mount turns every unmounted frame with data-map under root into a map.
  function mount(root) {
    var frames = [].slice.call((root || document).querySelectorAll('.map-frame[data-map]:not([data-mounted])'));
    if (!frames.length) { return Promise.resolve(); }
    return load().then(function (maplibre) {
      frames.forEach(function (el) {
        el.setAttribute('data-mounted', '');
        var data = JSON.parse(el.getAttribute('data-map') || '{}');
        var lang = (document.documentElement.lang || 'en').slice(0, 2);
        var map = new maplibre.Map({ container: el, style: style(el.getAttribute('data-map-source'), lang),
          attributionControl: { compact: true }, scrollZoom: false, dragRotate: false, pitchWithRotate: false });
        map.addControl(new maplibre.NavigationControl({ showCompass: false }), 'top-right');
        map.on('load', function () { draw(maplibre, map, el, data); });
      });
    });
  }

  // install joins window.Kante once the page is parsed: shrippen.js creates it then, and pages
  // wait for window.Kante to know that shrippen.js ran.
  function install() {
    (window.Kante = window.Kante || {}).map = { mount: mount };
    mount(document);
  }
  if (document.readyState !== 'loading') { install(); } else { document.addEventListener('DOMContentLoaded', install); }
})();
