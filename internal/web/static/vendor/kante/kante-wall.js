/* Kante wall: transitions between two sets of a wall display (.wall-stage > .wall-set).
 *
 *   Kante.wall.run(name, from, to, { easing: "standard", tiles: ".wall-tile", force: false }) → Promise
 *
 *   name     fade | cut | stagger | flap | shutter | scan (Kante.wall.names);
 *            Kante.wall.pick("rotate", n) gives the n-th of them, for a wall that alternates
 *   from     the set shown now; to: the next set, after `from` in the stage (on top) and visible
 *   easing   a key of Kante.wall.easings ("standard" keeps each transition's own curve) or a CSS easing
 *   tiles    selector of the tiles inside a set, for stagger and flap
 *   force    play even with prefers-reduced-motion: reduce (an explicit preview); without it the
 *            sets switch at once
 *
 * The promise resolves when `to` alone shows: the caller then hides or removes `from`. Every
 * style a transition sets is gone by then; the overlays (.wall-blade, .wall-scan) go into the
 * stage (the sets' parent) and are removed again.
 */
(function () {
  'use strict';

  // easings by key; a value starting with -- is read from Kante's tokens
  var EASINGS = {
    standard: '',
    linear: 'linear',
    quad: 'cubic-bezier(.45,0,.55,1)',
    cubic: 'cubic-bezier(.65,0,.35,1)',
    expo: 'cubic-bezier(.87,0,.13,1)',
    soft: 'cubic-bezier(.2,.7,.2,1)',
    snap: '--ease-snap'
  };
  var NAMES = ['cut', 'fade', 'stagger', 'shutter', 'flap', 'scan'];
  var SOFT = 'cubic-bezier(.2,.7,.2,1)';
  var FLAP_ROWS = 6;      // rows of a screen the split-flap turns one after another
  var BLADE_FROM = '-12%';
  var BLADE_TO = '118%';

  function easingOf(key) {
    var v = Object.prototype.hasOwnProperty.call(EASINGS, key) ? EASINGS[key] : key;
    if (v && v.indexOf('--') === 0) {
      return getComputedStyle(document.documentElement).getPropertyValue(v).trim() || 'ease';
    }
    return v || '';
  }

  function reduced() {
    return window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  }

  function done(a) { return a.finished.catch(function () {}); }

  function overlay(stage, cls) {
    var el = document.createElement('div');
    el.className = cls;
    el.setAttribute('aria-hidden', 'true');
    stage.appendChild(el);
    return el;
  }

  // tiles of a set that show, top first, then left
  function tilesOf(set, sel) {
    return [].filter.call(set.querySelectorAll(sel), function (t) { return t.getClientRects().length > 0; })
      .map(function (t) { return { el: t, r: t.getBoundingClientRect() }; })
      .sort(function (a, b) { return (a.r.top - b.r.top) || (a.r.left - b.r.left); });
  }

  // played: the animations of the running transition; their fill holds until all are done,
  // then run cancels them, so no style stays behind
  var played = [];

  function play(el, frames, o) {
    var a = el.animate(frames, { duration: o.duration, delay: o.delay || 0, easing: o.easing, fill: 'both' });
    played.push(a);
    return done(a);
  }

  // T: the transitions; e(own) is the chosen easing or the transition's own
  var T = {
    fade: function (s, a, b, e) {
      return Promise.all([
        play(a, [{ opacity: 1 }, { opacity: 0 }], { duration: 400, easing: e(SOFT) }),
        play(b, [{ opacity: 0 }, { opacity: 1 }], { duration: 450, delay: 250, easing: e(SOFT) })
      ]);
    },
    cut: function (s, a, b, e) {
      var blade = overlay(s, 'wall-blade');
      var o = { duration: 900, easing: e('cubic-bezier(.6,0,.3,1)') };
      return Promise.all([
        play(b, [{ clipPath: 'polygon(-30% 0, -30% 0, -50% 100%, -50% 100%)' }, { clipPath: 'polygon(-30% 0, 130% 0, 110% 100%, -50% 100%)' }], o),
        play(blade, [{ left: BLADE_FROM }, { left: BLADE_TO }], o)
      ]).then(function () { blade.remove(); });
    },
    stagger: function (s, a, b, e, sel) {
      b.style.visibility = 'hidden';
      var out = tilesOf(a, sel).map(function (t, i) {
        return play(t.el, [{ opacity: 1, transform: 'none' }, { opacity: 0, transform: 'translateY(-1.2cqw) scale(.98)' }], { duration: 260, delay: i * 35, easing: e(SOFT) });
      });
      return Promise.all(out).then(function () {
        var tiles = tilesOf(b, sel);
        var ins = tiles.map(function (t, i) {
          return play(t.el, [{ opacity: 0, transform: 'translateY(1.6cqw)' }, { opacity: 1, transform: 'none' }], { duration: 380, delay: i * 55, easing: e(SOFT) });
        });
        b.style.visibility = '';
        return Promise.all(ins);
      });
    },
    flap: function (s, a, b, e, sel) {
      var height = s.clientHeight || 1, top = s.getBoundingClientRect().top;
      var row = function (t) { return Math.round((t.r.top - top) / height * FLAP_ROWS) * 90; };
      b.style.visibility = 'hidden';
      var out = tilesOf(a, sel).map(function (t) {
        return play(t.el, [{ transformOrigin: '50% 0', transform: 'perspective(120cqw) rotateX(0)' }, { transformOrigin: '50% 0', transform: 'perspective(120cqw) rotateX(90deg)', opacity: 0.4 }],
          { duration: 260, delay: row(t), easing: e('cubic-bezier(.5,0,.9,.4)') });
      });
      return Promise.all(out).then(function () {
        var tiles = tilesOf(b, sel);
        var ins = tiles.map(function (t) {
          return play(t.el, [{ transformOrigin: '50% 0', transform: 'perspective(120cqw) rotateX(-90deg)', opacity: 0.4 }, { transformOrigin: '50% 0', transform: 'perspective(120cqw) rotateX(0)', opacity: 1 }],
            { duration: 340, delay: row(t), easing: e('cubic-bezier(.2,.9,.3,1.2)') });
        });
        b.style.visibility = '';
        return Promise.all(ins);
      });
    },
    shutter: function (s, a, b, e) {
      var o = { duration: 800, easing: e('cubic-bezier(.7,0,.2,1)') };
      return Promise.all([
        play(a, [{ transform: 'translateY(0)' }, { transform: 'translateY(-100%)' }], o),
        play(b, [{ transform: 'translateY(100%)' }, { transform: 'translateY(0)' }], o)
      ]);
    },
    scan: function (s, a, b, e) {
      var line = overlay(s, 'wall-scan');
      var o = { duration: 1000, easing: e('cubic-bezier(.45,.05,.35,1)') };
      return Promise.all([
        play(b, [{ clipPath: 'inset(0 0 100% 0)' }, { clipPath: 'inset(0 0 0% 0)' }], o),
        play(line, [{ top: '0%' }, { top: '100%' }], o)
      ]).then(function () { line.remove(); });
    }
  };

  function run(name, from, to, opts) {
    opts = opts || {};
    var fn = T[name] || T.cut;
    if (!from || !to.animate || (!opts.force && reduced())) {
      return Promise.resolve();
    }
    var chosen = easingOf(opts.easing || 'standard');
    var e = function (own) { return chosen || own; };
    played = [];
    return fn(to.parentElement, from, to, e, opts.tiles || '.wall-tile').then(function () {
      var all = played;
      played = [];
      // to stays on top, so from may come back underneath for the moment the caller needs to hide it
      all.forEach(function (a) { a.cancel(); });
    });
  }

  // pick: the transition for the n-th change; "rotate" takes them in turn
  function pick(name, n) {
    if (name === 'rotate') {
      return NAMES[((n % NAMES.length) + NAMES.length) % NAMES.length];
    }
    return T[name] ? name : 'cut';
  }

  (window.Kante = window.Kante || {}).wall = { run: run, pick: pick, names: NAMES.slice(), easings: Object.keys(EASINGS) };
})();
