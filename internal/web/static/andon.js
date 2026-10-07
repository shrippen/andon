/* Andon — search, hotkeys, clocks, folding, confirmations, soft page changes. No framework. */
(function () {
  "use strict";

  var d = document;
  var CLOCK_TICK_MS = 1000;

  var SEARCH_KEY = "/";

  function csrf() {
    var meta = d.querySelector('meta[name="csrf"]');
    return meta ? meta.getAttribute("content") || "" : "";
  }

  // POST without reload (fold state), CSRF via header.
  function post(url, fields) {
    var body = new URLSearchParams(fields || {});
    return fetch(url, {
      method: "POST",
      headers: { "X-CSRF-Token": csrf(), "Content-Type": "application/x-www-form-urlencoded" },
      body: body,
      credentials: "same-origin"
    });
  }

  function isTyping(target) {
    var tag = (target && target.tagName) || "";
    return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || (target && target.isContentEditable);
  }

  // ── Search: filter link tiles; arrows pick a hit, Enter opens it or the web search ──
  var selected = 0;

  function visibleTiles() {
    return [].filter.call(d.querySelectorAll(".launch"), function (a) {
      return !a.closest(".tile-slot").hidden;
    });
  }

  function filter(query) {
    var q = query.trim().toLowerCase();
    var hits = 0;

    [].forEach.call(d.querySelectorAll(".tile-slot"), function (slot) {
      var link = slot.querySelector(".launch");
      if (!link) {
        slot.hidden = q !== "";
        return;
      }
      var match = !q || (link.getAttribute("data-search") || "").toLowerCase().indexOf(q) >= 0;
      slot.hidden = !match;
      link.classList.remove("is-first");
      if (match) {
        hits += 1;
      }
    });

    [].forEach.call(d.querySelectorAll(".dsec"), function (sec) {
      var any = sec.querySelector(".tile-slot:not([hidden])");
      sec.hidden = q !== "" && !any;
      // A hit in a folded section opens it for now; data-open first, so
      // Kante sees no user change and nothing is stored.
      var fold = sec.querySelector(".fold");
      if (q && fold && !fold.open) {
        fold.dataset.open = "true";
        fold.open = true;
      }
    });

    selected = 0;
    mark();

    var empty = d.querySelector(".search-empty");
    if (empty) {
      empty.hidden = !(q && hits === 0);
    }
  }

  // mark highlights the selected hit ("is-first" keeps its old name).
  function mark() {
    var input = d.getElementById("search");
    var searching = input && input.value.trim() !== "";
    visibleTiles().forEach(function (a, i) {
      a.classList.toggle("is-first", searching && i === selected);
    });
  }

  // move steps the selection by delta, wrapping at both ends.
  function move(delta) {
    var tiles = visibleTiles();
    if (!tiles.length) {
      return;
    }
    selected = (selected + delta + tiles.length) % tiles.length;
    mark();
    tiles[selected].scrollIntoView({ block: "nearest" });
  }

  function openSearch(input) {
    var q = input.value.trim();
    if (!q) {
      return;
    }

    var hit = visibleTiles()[selected];
    if (hit) {
      hit.click();
      return;
    }

    var engine = input.getAttribute("data-engine");
    if (engine) {
      window.location.href = engine.replace("{query}", encodeURIComponent(q));
    }
  }

  // firstBind marks el as set up: a morphed page change keeps elements,
  // and their listeners must not double.
  function firstBind(el) {
    if (!el || el.andonBound) {
      return false;
    }
    el.andonBound = true;
    return true;
  }

  function setupSearch() {
    var input = d.getElementById("search");
    if (!firstBind(input)) {
      return;
    }

    input.addEventListener("input", function () {
      filter(input.value);
    });
    input.addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        e.preventDefault();
        openSearch(input);
      }
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        move(e.key === "ArrowDown" ? 1 : -1);
      }
      if (e.key === "Escape") {
        input.value = "";
        filter("");
        input.blur();
      }
    });
  }

  // ── Hotkeys: "/" focuses search, tile hotkeys open links ──
  function setupHotkeys() {
    d.addEventListener("keydown", function (e) {
      if (e.ctrlKey || e.metaKey || e.altKey || isTyping(e.target)) {
        return;
      }

      if (e.key === SEARCH_KEY) {
        var input = d.getElementById("search");
        if (input) {
          e.preventDefault();
          input.focus();
        }
        return;
      }

      var tile = d.querySelector('.launch[data-hotkey="' + CSS.escape(e.key) + '"]');
      if (tile) {
        e.preventDefault();
        tile.click();
      }
    });
  }

  // ── Hints page: / searches, j / k walk the hints, a marks the focused
  // one done, s pauses it for 7 days ──
  // Only the hint's own text counts, not its buttons and form labels
  // ("Pausieren", "Notiz" are on every card).
  var hintText = "header, .title, .hint-tags, .hint-why, .hint-meta-line";

  function filterHints(query) {
    var q = query.trim().toLowerCase();
    [].forEach.call(d.querySelectorAll(".hints-page li.hint-card"), function (li) {
      var text = [].map.call(li.querySelectorAll(hintText), function (el) { return el.textContent; }).join(" ");
      li.hidden = q !== "" && text.toLowerCase().indexOf(q) < 0;
    });
    [].forEach.call(d.querySelectorAll(".hints-page .hint-rest"), function (rest) {
      rest.open = rest.open || q !== "";
    });
    [].forEach.call(d.querySelectorAll(".hints-page .hint-rule"), function (sec) {
      sec.hidden = q !== "" && !sec.querySelector("li.hint-card:not([hidden])");
    });
  }

  function setupHintKeys() {
    d.addEventListener("input", function (e) {
      if (e.target.id === "hint-search") {
        filterHints(e.target.value);
      }
    });
    d.addEventListener("keydown", function (e) {
      if (!d.querySelector(".hints-page") || e.ctrlKey || e.metaKey || e.altKey || isTyping(e.target)) {
        return;
      }
      var list = [].filter.call(d.querySelectorAll(".hints-page li.hint-card[id]"), function (li) {
        return li.offsetParent !== null;
      });
      var current = d.activeElement && d.activeElement.closest ? d.activeElement.closest("li.hint-card") : null;
      var at = list.indexOf(current);
      var target = null;
      switch (e.key) {
        case SEARCH_KEY:
          target = d.getElementById("hint-search");
          break;
        case "j":
          target = list[Math.min(at + 1, list.length - 1)];
          break;
        case "k":
          target = list[Math.max(at - 1, 0)];
          break;
        case "a":
        case "s":
          if (current) {
            var btn = current.querySelector(e.key === "a" ? "form.hint-ack button:not([formaction])" : "form.hint-ack button[formaction]");
            if (btn) {
              e.preventDefault();
              btn.click();
            }
          }
          return;
        default:
          return;
      }
      if (!target) {
        return;
      }
      e.preventDefault();
      if (target.tagName !== "INPUT") {
        target.setAttribute("tabindex", "-1");
        target.scrollIntoView({ block: "nearest" });
      }
      target.focus();
    });
  }

  // ── Receipts page: j / k walk the suggestions, Enter links the focused
  // one, n jumps to the next expense, s opens its search ──
  function setupReceiptKeys() {
    function focusOn(el) {
      if (!el) {
        return;
      }
      el.setAttribute("tabindex", "-1");
      el.focus();
      el.scrollIntoView({ block: "nearest" });
    }

    // A search or form panel empties its slot on "close" or Esc; the
    // focus goes back to the card.
    function closeSearch(from) {
      var page = d.querySelector("main.receipts");
      var panel = from && from.closest ? from.closest(".receipt-search") : null;
      panel = panel || (page && page.querySelector(".receipt-search"));
      if (!panel || !panel.parentElement) {
        return;
      }
      var card = panel.closest(".receipt-match");
      panel.parentElement.innerHTML = "";
      focusOn(card && (card.querySelector(".kb-item") || card));
    }

    d.addEventListener("click", function (e) {
      var btn = e.target.closest && e.target.closest("[data-close-search]");
      if (btn) {
        closeSearch(btn);
      }
    });

    // Scans ticked for a split receipt: their sum against the expense.
    // The zero amount the server rendered ("0,00 €") gives the format.
    function pickSum(form) {
      var boxes = d.querySelectorAll('input[data-pick][form="' + form.id + '"]');
      var sum = 0, count = 0;
      Array.prototype.forEach.call(boxes, function (box) {
        if (box.checked) {
          sum += parseFloat(box.getAttribute("data-amount")) || 0;
          count++;
        }
      });
      var out = form.querySelector("[data-pick-sum]");
      var lang = d.documentElement.lang || "de";
      var number = sum.toLocaleString(lang, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
      out.textContent = out.getAttribute("data-zero").replace(/[\d.,\s]*\d/, number);
      var state = form.querySelector("[data-pick-state]");
      var fits = Math.abs(sum - parseFloat(form.getAttribute("data-target"))) <= 0.02;
      state.hidden = count === 0;
      state.setAttribute("data-state", fits ? "ok" : "warn");
      state.textContent = state.getAttribute(fits ? "data-ok" : "data-off");
      form.querySelector("button[type=submit]").disabled = count === 0;
    }

    d.addEventListener("change", function (e) {
      if (e.target.matches && e.target.matches("input[data-pick]")) {
        var form = d.getElementById(e.target.getAttribute("form"));
        if (form) {
          pickSum(form);
        }
      }
    });

    d.addEventListener("htmx:afterSwap", function () {
      Array.prototype.forEach.call(d.querySelectorAll("form.receipt-pick"), pickSum);
    });

    // A scan without thumbnail leaves no empty frame behind.
    d.addEventListener("error", function (e) {
      var frame = e.target && e.target.closest ? e.target.closest(".receipt-thumb") : null;
      if (frame) {
        frame.hidden = true;
      }
    }, true);

    d.addEventListener("keydown", function (e) {
      var page = d.querySelector("main.receipts");
      if (page && e.key === "Escape" && e.target.closest && e.target.closest(".receipt-search")) {
        closeSearch(e.target);
        return;
      }
      if (!page || e.ctrlKey || e.metaKey || e.altKey || isTyping(e.target)) {
        return;
      }
      var items = Array.prototype.slice.call(page.querySelectorAll(".kb-item"));
      var here = d.activeElement && d.activeElement.closest ? d.activeElement.closest(".kb-item") : null;
      var at = items.indexOf(here);
      var card = d.activeElement && d.activeElement.closest ? d.activeElement.closest(".receipt-match") : null;

      if (e.key === "j" || e.key === "k") {
        if (!items.length) {
          return;
        }
        e.preventDefault();
        var step = e.key === "j" ? 1 : -1;
        focusOn(items[(at + step + items.length) % items.length]);
      } else if (e.key === "Enter" && here && d.activeElement === here) {
        var link = here.querySelector("button[data-link]");
        if (link) {
          e.preventDefault();
          link.click();
        }
      } else if (e.key === "n") {
        var cards = Array.prototype.slice.call(page.querySelectorAll(".receipt-match"));
        if (!cards.length) {
          return;
        }
        e.preventDefault();
        var next = cards[(cards.indexOf(card) + 1) % cards.length];
        focusOn(next.querySelector(".kb-item") || next);
      } else if (e.key === "Escape") {
        closeSearch(d.activeElement);
      } else if (e.key === "s" && card) {
        var search = card.querySelector("[data-search]");
        if (search) {
          e.preventDefault();
          search.click();
        }
      }
    });
  }

  // ── Context menu on links: new tab, same tab, copy address ──
  // Shift + right click keeps the browser's own menu.
  function setupContextMenu() {
    var target = "";

    function close() {
      var menu = d.getElementById("ctx-menu");
      if (menu) {
        menu.hidden = true;
      }
    }

    d.addEventListener("contextmenu", function (e) {
      var menu = d.getElementById("ctx-menu");
      var link = e.target.closest && e.target.closest(".launch, .launch-items a");
      if (!menu || !link || e.shiftKey) {
        close();
        return;
      }
      e.preventDefault();
      target = link.href;
      menu.hidden = false;

      // Keep the menu inside the viewport.
      var x = Math.min(e.clientX, window.innerWidth - menu.offsetWidth - 4);
      var y = Math.min(e.clientY, window.innerHeight - menu.offsetHeight - 4);
      menu.style.left = Math.max(0, x) + "px";
      menu.style.top = Math.max(0, y) + "px";
      menu.querySelector("button").focus();
    });

    d.addEventListener("click", function (e) {
      var btn = e.target.closest && e.target.closest("#ctx-menu button");
      if (!btn) {
        return;
      }
      var act = btn.getAttribute("data-act");
      if (act === "newtab") {
        window.open(target, "_blank", "noopener");
      } else if (act === "sametab") {
        window.location.href = target;
      } else if (act === "copy" && navigator.clipboard) {
        navigator.clipboard.writeText(target);
      }
      close();
    });

    d.addEventListener("click", function (e) {
      if (!(e.target.closest && e.target.closest("#ctx-menu"))) {
        close();
      }
    });
    d.addEventListener("keydown", function (e) {
      if (e.key === "Escape") {
        close();
      }
    });
    window.addEventListener("scroll", close, { passive: true });
    window.addEventListener("blur", close);
  }

  // ── Command palette (Ctrl+K) and shortcut help (?) ──
  var PALETTE_PER_GROUP = 5;
  var paletteItems = null;
  var paletteSel = 0;

  // paletteMatches keeps the server's grouped order and a few hits per group.
  function paletteMatches(q) {
    q = q.trim().toLowerCase();
    var per = {};
    return (paletteItems || []).filter(function (it) {
      if (q && (it.title + " " + (it.detail || "") + " " + it.url).toLowerCase().indexOf(q) < 0) {
        return false;
      }
      per[it.kind] = (per[it.kind] || 0) + 1;
      return per[it.kind] <= PALETTE_PER_GROUP;
    });
  }

  function renderPalette() {
    var list = d.getElementById("palette-list");
    var q = d.getElementById("palette-q").value;
    var hits = paletteMatches(q);
    paletteSel = Math.min(paletteSel, Math.max(hits.length - 1, 0));
    list.innerHTML = "";
    var group = null;
    hits.forEach(function (it, i) {
      if (it.group && it.group !== group) {
        group = it.group;
        var head = d.createElement("li");
        head.setAttribute("role", "presentation");
        head.className = "palette-group";
        head.textContent = group;
        list.appendChild(head);
      }
      var li = d.createElement("li");
      li.setAttribute("role", "option");
      li.setAttribute("aria-selected", String(i === paletteSel));
      li.dataset.kind = it.kind;
      var a = d.createElement("a");
      a.href = it.url;
      if (it.kind === "link") {
        a.target = "_blank";
        a.rel = "noopener noreferrer";
      }
      a.textContent = it.title;
      li.appendChild(a);
      if (it.detail) {
        var small = d.createElement("small");
        small.textContent = it.detail;
        li.appendChild(small);
      }
      list.appendChild(li);
    });
    var sel = list.querySelector('li[aria-selected="true"]');
    if (sel && sel.scrollIntoView) {
      sel.scrollIntoView({ block: "nearest" });
    }
  }

  function openPalette() {
    var dlg = d.getElementById("palette");
    if (!dlg || dlg.open) {
      return;
    }
    dlg.showModal();
    var input = d.getElementById("palette-q");
    input.value = "";
    paletteSel = 0;
    if (paletteItems) {
      renderPalette();
      return;
    }
    fetch("/palette.json", { credentials: "same-origin" }).then(function (r) {
      return r.ok ? r.json() : [];
    }).then(function (items) {
      paletteItems = items || [];
      renderPalette();
    });
  }

  // bindPalette wires the palette input of the current page.
  function bindPalette() {
    paletteItems = null;
    var dlg = d.getElementById("palette");
    if (!dlg) {
      return;
    }
    var input = d.getElementById("palette-q");
    if (!firstBind(input)) {
      return;
    }
    input.addEventListener("input", function () {
      paletteSel = 0;
      renderPalette();
    });
    input.addEventListener("keydown", function (e) {
      var count = d.querySelectorAll('#palette-list li[role="option"]').length;
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        if (count) {
          paletteSel = (paletteSel + (e.key === "ArrowDown" ? 1 : -1) + count) % count;
          renderPalette();
        }
      }
      if (e.key === "Enter") {
        e.preventDefault();
        var link = d.querySelector('#palette-list li[aria-selected="true"] a');
        if (link) {
          link.click();
          dlg.close();
        }
      }
    });
  }

  function setupPalette() {
    d.addEventListener("click", function (e) {
      if (e.target.closest && e.target.closest('[data-open="palette"]')) {
        openPalette();
      }
    });
    d.addEventListener("keydown", function (e) {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        openPalette();
        return;
      }
      if (e.key === "?" && !isTyping(e.target)) {
        var help = d.getElementById("shortcuts");
        if (help && !help.open) {
          e.preventDefault();
          help.showModal();
        }
      }
    });
  }

  // ── Click counting for "frequently used" (beacon: never delays navigation) ──
  function setupClicks() {
    d.addEventListener("click", function (e) {
      var link = e.target.closest && e.target.closest("[data-click]");
      if (!link || !navigator.sendBeacon) {
        return;
      }
      var form = new FormData();
      form.append("csrf", csrf());
      navigator.sendBeacon("/clicks/" + link.getAttribute("data-click"), form);
    });
  }

  // ── Clocks ──
  // Kimai Lite: running timers count up from their begin.
  function pad(n) { return n < 10 ? "0" + n : String(n); }

  function tickTimers() {
    [].forEach.call(d.querySelectorAll(".kl-time[data-begin]"), function (el) {
      var begin = Date.parse(el.getAttribute("data-begin"));
      if (isNaN(begin)) {
        return;
      }
      var s = Math.max(0, Math.floor((Date.now() - begin) / 1000));
      el.textContent = Math.floor(s / 3600) + ":" + pad(Math.floor(s / 60) % 60) + ":" + pad(s % 60);
    });
  }

  // Embedded pages with data-reload (minutes) load again at that pace.
  function reloadFrames() {
    var now = Date.now();
    [].forEach.call(d.querySelectorAll("iframe[data-reload]"), function (f) {
      var every = parseFloat(f.getAttribute("data-reload")) * 60000;
      var since = parseFloat(f.getAttribute("data-loaded") || "0");
      if (!since) {
        f.setAttribute("data-loaded", String(now));
      } else if (every > 0 && now - since >= every) {
        f.setAttribute("data-loaded", String(now));
        f.src = f.src;
      }
    });
  }

  function tick() {
    tickTimers();
    reloadFrames();
    [].forEach.call(d.querySelectorAll("[data-clock]"), function (el) {
      var zone = el.getAttribute("data-tz");
      var locale = el.getAttribute("data-locale") || undefined;
      var seconds = el.getAttribute("data-seconds") === "yes";
      var now = new Date();
      var time = { hour: "2-digit", minute: "2-digit", timeZone: zone };
      if (seconds) {
        time.second = "2-digit";
      }
      // Say 12 or 24 hours either way: left open, an English locale
      // picks 12 hours ("03:08 PM") for a 24-hour clock.
      if (el.getAttribute("data-h12") === "yes") {
        time.hour = "numeric";
        time.hour12 = true;
      } else {
        time.hourCycle = "h23";
      }
      var face = el.querySelector("svg.clock");
      if (face) {
        try {
          var hms = now.toLocaleTimeString("en-GB", { hour12: false, timeZone: zone }).split(":").map(Number);
          var m = hms[1] + hms[2] / 60;
          face.querySelector(".h").style.setProperty("--a", ((hms[0] % 12) * 30 + m / 2) + "deg");
          face.querySelector(".m").style.setProperty("--a", (m * 6) + "deg");
          var sec = face.querySelector(".s");
          if (sec) {
            sec.style.setProperty("--a", (hms[2] * 6) + "deg");
          }
        } catch (err) { /* unknown zone: the text says "?" below */ }
      }

      try {
        el.querySelector(".clock-time").textContent = now.toLocaleTimeString(locale, time);
        var date = el.querySelector(".clock-date");
        if (date) {
          date.textContent = now.toLocaleDateString(locale, { weekday: "long", day: "numeric", month: "long", timeZone: zone });
        }
      } catch (err) {
        el.querySelector(".clock-time").textContent = "?";
      }
    });
  }

  // ── Folding: Kante's details.fold folds; the change goes to the
  // personal overlay (data-fold is the section's fold route). ──
  function setupFolding() {
    d.addEventListener("kante:fold", function (e) {
      var url = e.target.getAttribute && e.target.getAttribute("data-fold");
      if (!url) {
        return;
      }
      post(url, { state: e.detail.open ? "open" : "closed" });
    });
  }

  // ── Confirm destructive forms (no inline handlers: CSP) ──
  function setupConfirm() {
    d.addEventListener("submit", function (e) {
      var form = e.target.closest && e.target.closest("form[data-confirm]");
      if (form && !window.confirm(form.getAttribute("data-confirm"))) {
        e.preventDefault();
        e.stopPropagation();
      }
    }, true);
  }

  // ── Edit actions keep the place ──
  //
  // Board edits answer with the board again; a long board jumped to the
  // top after every change. The tile (or section) acted on returns to its
  // spot on screen. By element, not scrollY: tiles off screen come back
  // with placeholder heights (content-visibility).
  //
  //   submit ─► remember {path, tile, section, top} ─► new page ─► restore
  var PLACE_KEY = "andon-place";
  var PLACE_MAX_AGE_MS = 30000;

  function placeOf(el) {
    var tile = el.closest && el.closest("[data-placement]");
    var section = el.closest && el.closest("[data-section]");
    if (!tile && !section) {
      return null;
    }
    return {
      path: window.location.pathname, at: Date.now(),
      tile: tile ? tile.getAttribute("data-placement") : "", tileTop: tile ? tile.getBoundingClientRect().top : 0,
      section: section ? section.getAttribute("data-section") : "", sectionTop: section ? section.getBoundingClientRect().top : 0
    };
  }

  function setupPlace() {
    d.addEventListener("submit", function (e) {
      var place = placeOf(e.submitter || e.target);
      try {
        if (place) {
          window.sessionStorage.setItem(PLACE_KEY, JSON.stringify(place));
        } else {
          window.sessionStorage.removeItem(PLACE_KEY);
        }
      } catch (err) { /* storage blocked: jump to the top as before */ }
    }, true);
  }

  function forgetPlace() {
    try {
      window.sessionStorage.removeItem(PLACE_KEY);
    } catch (err) { /* nothing stored */ }
  }

  function restorePlace() {
    var place = null;
    try {
      place = JSON.parse(window.sessionStorage.getItem(PLACE_KEY) || "null");
      window.sessionStorage.removeItem(PLACE_KEY);
    } catch (err) {
      return;
    }
    if (!place || place.path !== window.location.pathname || Date.now() - place.at > PLACE_MAX_AGE_MS) {
      return;
    }
    // A removed tile: fall back to its section.
    var el = place.tile && d.querySelector('[data-placement="' + place.tile + '"]');
    var top = place.tileTop;
    if (!el) {
      el = place.section && d.querySelector('[data-section="' + place.section + '"]');
      top = place.sectionTop;
    }
    if (!el) {
      return;
    }
    // After htmx's own scroll to the top of a boosted page.
    window.requestAnimationFrame(function () {
      el.scrollIntoView({ block: "start" });
      window.scrollBy(0, -top);
    });
  }

  // ── Selects that submit their form on change (no inline handlers: CSP) ──
  function setupAutosubmit() {
    d.addEventListener("change", function (e) {
      var el = e.target.closest && e.target.closest("[data-autosubmit]");
      if (el && el.form) {
        el.form.requestSubmit();
      }
    });
  }

  // ── Header menus and dropdowns (<details>): one open at a time, closed by outside click or Esc ──
  var MENUS = "details.nav-menu[open], details.dropdown[open]";
  function setupMenus() {
    d.addEventListener("click", function (e) {
      d.querySelectorAll(MENUS).forEach(function (m) {
        if (!m.contains(e.target)) {
          m.open = false;
        }
      });
    });
    d.addEventListener("keydown", function (e) {
      if (e.key === "Escape") {
        d.querySelectorAll(MENUS).forEach(function (m) { m.open = false; });
      }
    });
  }

  // ── Hint badge on a link tile: show that connection's hints instead of following the link ──
  var HINT_POP_WIDTH = 360, HINT_POP_GAP = 6, HINT_POP_MARGIN = 8;

  function hintPop() {
    var pop = d.querySelector(".hint-pop");
    if (!pop) {
      pop = d.createElement("div");
      pop.className = "hint-pop";
      pop.setAttribute("popover", "");
      d.body.appendChild(pop);
    }
    return pop;
  }

  function setupHintPop() {
    d.addEventListener("click", function (e) {
      var badge = e.target.closest && e.target.closest(".launch-hints[data-hints]");
      if (!badge) {
        return;
      }
      e.preventDefault();
      e.stopPropagation();

      fetch(badge.getAttribute("data-hints"), { credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.text() : ""; })
        .then(function (html) {
          // Server-rendered html/template output from our own origin.
          var pop = hintPop();
          pop.innerHTML = html;
          applyStyles(pop);
          var box = badge.getBoundingClientRect();
          var left = Math.min(box.left, window.innerWidth - HINT_POP_WIDTH - HINT_POP_MARGIN);
          pop.style.top = Math.round(box.bottom + HINT_POP_GAP) + "px";
          pop.style.left = Math.round(Math.max(HINT_POP_MARGIN, left)) + "px";
          if (pop.showPopover) {
            pop.showPopover();
          }
        });
    }, true);
  }

  // ── Detail dialogs: one dialog for every tile type (templates/details.html) ──
  //
  //   [data-details="/details/42"] ─fetch─► <dialog id="detail" class="dialog detail">
  //   [data-pick] > button: data-v-NAME ─► text of [data-v="NAME"], data-state-NAME ─► its
  //                         data-state, data-x ─► x1/x2 of [data-pick-x] (the chosen day's mark)
  //   [data-detail-refresh] ─► forced fetch of the tile, then the dialog anew
  //
  // A trigger may sit inside a tile's link, so it stops the link like the hint badge.
  var DETAIL_WAIT = '<div class="detail-wait"><span class="loader" aria-hidden="true"><i></i><i></i><i></i></span></div>';
  var detailURL = "";

  function detailDialog() {
    var dlg = d.getElementById("detail");
    if (!dlg) {
      dlg = d.createElement("dialog");
      dlg.id = "detail";
      dlg.className = "dialog detail";
      dlg.setAttribute("aria-labelledby", "detail-title");
      // Its forms post through setupEditor, never as a soft page change.
      dlg.setAttribute("hx-boost", "false");
      dlg.addEventListener("cancel", function (e) {
        if (!leaveOK(dlg)) {
          e.preventDefault();
        }
      });
      d.body.appendChild(dlg);
    }
    return dlg;
  }

  // fillDetail puts the server's answer into the dialog and readies it:
  // styles, maps, htmx (the editor's live preview), the first field.
  function fillDetail(dlg, html) {
    // Server-rendered html/template output from our own origin.
    dlg.innerHTML = html;
    applyStyles(dlg);
    mountMaps(dlg);
    if (window.htmx) {
      htmx.process(dlg);
    }
    var first = dlg.querySelector("[autofocus]") ||
      dlg.querySelector("#widget-form fieldset :is(input:not([type=hidden]), select, textarea)");
    if (first) {
      first.focus();
    }
  }

  // leaveOK asks before the editor's unsaved changes are dropped.
  function leaveOK(dlg) {
    var form = dlg.querySelector("form[data-dirty][data-changed]");
    return !form || window.confirm(form.getAttribute("data-dirty"));
  }

  // A click on the backdrop closes the dialog like Escape does (its cancel
  // handlers may veto). Press and release must both land outside the box, so
  // a text selection dragged out of it keeps the dialog. The box's padding
  // counts as inside: the click target is the dialog itself there too.
  var pressedOutside = null;

  function outside(dlg, e) {
    var r = dlg.getBoundingClientRect();
    return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
  }

  d.addEventListener("pointerdown", function (e) {
    var dlg = e.target instanceof HTMLDialogElement ? e.target : null;
    pressedOutside = dlg && outside(dlg, e) ? dlg : null;
  }, true);

  d.addEventListener("click", function (e) {
    var dlg = pressedOutside;
    pressedOutside = null;
    if (!dlg || e.target !== dlg || !dlg.open || !outside(dlg, e)) {
      return;
    }
    if (dlg.dispatchEvent(new Event("cancel", { cancelable: true }))) {
      dlg.close();
    }
  });

  // openDetail shows the frame at once and fills it when the answer is in;
  // a later open wins over an earlier one still on its way.
  function openDetail(url) {
    var dlg = detailDialog();
    if (dlg.open && !leaveOK(dlg)) {
      return;
    }
    detailURL = url;
    if (!dlg.open) {
      dlg.innerHTML = DETAIL_WAIT;
      dlg.setAttribute("aria-busy", "true");
      dlg.showModal();
    }
    fetch(url, { credentials: "same-origin" })
      .then(function (r) { return r.ok ? r.text() : ""; })
      .then(function (html) {
        if (url !== detailURL) {
          return;
        }
        dlg.removeAttribute("aria-busy");
        if (!html) {
          dlg.close();
          return;
        }
        fillDetail(dlg, html);
      });
  }

  // ── The widget editor: only ever in the detail dialog ──
  //
  //   [data-details="/widgets/7/edit?dialog"] ─► dialog: form + live preview
  //   submit ─fetch─┬─ redirected (saved) ─► the page it names, back at the tile
  //                 └─ answer (refused)   ─► the dialog anew, with the message
  //   ?editor=/widgets/…  a page opens that editor on load (its plain address)
  //
  // Any post form in the dialog goes this way: the editor, its delete, the
  // gallery's reuse forms. The place is the tile or section it opened from.
  var editorPlace = null;
  var EDITOR_PARAM = /([?&])editor=([^&]*)(&|$)/;

  function setupEditor() {
    d.addEventListener("input", markChanged);
    d.addEventListener("change", markChanged);
    // Bubbling: setupConfirm (capturing) may have cancelled a delete.
    d.addEventListener("submit", function (e) {
      var form = e.target.closest && e.target.closest('#detail form[method="post"]:not([data-detail-form])');
      if (!form || e.defaultPrevented) {
        return;
      }
      e.preventDefault();
      var button = e.submitter;
      if (button) {
        button.disabled = true;
      }
      fetch(form.getAttribute("action"), {
        method: "POST",
        headers: { "X-CSRF-Token": csrf() },
        body: new URLSearchParams(new FormData(form)),
        credentials: "same-origin"
      }).then(function (r) {
        if (r.redirected) {
          keepPlace();
          window.location.assign(r.url);
          return;
        }
        return r.text().then(function (html) {
          if (button) {
            button.disabled = false;
          }
          fillDetail(detailDialog(), html);
        });
      });
    });
  }

  function markChanged(e) {
    var form = e.target.closest && e.target.closest("#detail form[data-dirty]");
    if (form) {
      form.setAttribute("data-changed", "");
    }
  }

  // keepPlace hands the editor's origin to restorePlace on the next page.
  function keepPlace() {
    if (!editorPlace) {
      return;
    }
    editorPlace.at = Date.now();
    try {
      window.sessionStorage.setItem(PLACE_KEY, JSON.stringify(editorPlace));
    } catch (err) { /* storage blocked: the page starts at the top */ }
  }

  // openEditorParam opens the editor a page was asked for and drops the
  // parameter from the address: a reload must not open it again.
  function openEditorParam() {
    var m = EDITOR_PARAM.exec(window.location.search);
    if (!m) {
      return;
    }
    var url = decodeURIComponent(m[2]);
    var rest = window.location.search.replace(EDITOR_PARAM, function (all, before, value, after) { return after ? before : ""; });
    window.history.replaceState(window.history.state, "", window.location.pathname + rest + window.location.hash);
    if (url.indexOf("/widgets/") === 0) {
      openDetail(url);
    }
  }

  // ── Icon upload (widget editor): store the file, put the returned spec into the icon field ──
  function setupIconUpload() {
    d.addEventListener("change", function (e) {
      var input = e.target;
      if (!input.classList || !input.classList.contains("icon-upload") || !input.files.length) {
        return;
      }
      var body = new FormData();
      body.append("file", input.files[0]);
      fetch("/icons/upload", {
        method: "POST",
        headers: { "X-CSRF-Token": csrf() },
        body: body,
        credentials: "same-origin"
      }).then(function (res) { return res.text().then(function (text) { return [res.ok, text]; }); })
        .then(function (pair) {
          if (!pair[0]) {
            window.alert(pair[1]);
            return;
          }
          var field = input.form.querySelector('[name="' + input.getAttribute("data-target") + '"]');
          field.value = pair[1];
          field.dispatchEvent(new Event("input", { bubbles: true }));
        });
    });
  }

  // countClick tells the server a link tile was opened (its dialog shows
  // the days); a beacon survives the page change.
  function countClick(e) {
    var a = e.target.closest && e.target.closest(".tile-slot.w-link a[href]");
    if (!a || a.hasAttribute("data-details") || !navigator.sendBeacon) {
      return;
    }
    var slot = a.closest(".tile-slot");
    var form = new FormData();
    form.append("csrf", csrf());
    navigator.sendBeacon("/widget-fragments/" + slot.getAttribute("data-placement") + "/click", form);
  }

  // doDetail runs an act of the dialog (mark done, add to a list) and
  // shows the dialog the server draws after it.
  function doDetail(btn) {
    var dlg = detailDialog();
    btn.disabled = true;
    post(btn.getAttribute("data-detail-do"))
      .then(function (r) { return r.ok ? r.text() : ""; })
      .then(function (html) {
        if (!html) {
          btn.disabled = false;
          btn.setAttribute("aria-invalid", "true");
          return;
        }
        // Server-rendered html/template output from our own origin.
        dlg.innerHTML = html;
        applyStyles(dlg);
        mountMaps(dlg);
      });
  }

  // pick shows one entry's values (a day of the link history) in the dialog.
  function pick(btn) {
    var dlg = btn.closest("dialog");
    [].forEach.call(btn.parentNode.children, function (b) {
      b.setAttribute("aria-pressed", b === btn ? "true" : "false");
    });
    [].forEach.call(btn.attributes, function (a) {
      var m = /^data-(v|state)-(.+)$/.exec(a.name);
      if (!m) {
        return;
      }
      dlg.querySelectorAll('[data-v="' + m[2] + '"]').forEach(function (el) {
        if (m[1] === "v") {
          el.textContent = a.value;
        } else {
          el.setAttribute("data-state", a.value);
        }
      });
    });
    var x = btn.getAttribute("data-x");
    if (x) {
      dlg.querySelectorAll("[data-pick-x]").forEach(function (el) {
        el.setAttribute("x1", x);
        el.setAttribute("x2", x);
      });
    }
  }

  function setupDetail() {
    d.addEventListener("click", countClick, true);
    // A dialog form posts like an act and shows the dialog drawn anew.
    d.addEventListener("submit", function (e) {
      var form = e.target.closest && e.target.closest("#detail form[data-detail-form]");
      if (!form) {
        return;
      }
      e.preventDefault();
      var fields = {};
      new FormData(form).forEach(function (v, k) { fields[k] = v; });
      var dlg = detailDialog();
      post(form.getAttribute("action"), fields)
        .then(function (r) { return r.ok ? r.text() : ""; })
        .then(function (html) {
          if (!html) {
            form.setAttribute("aria-invalid", "true");
            return;
          }
          // Server-rendered html/template output from our own origin.
          dlg.innerHTML = html;
          applyStyles(dlg);
          mountMaps(dlg);
        });
    }, true);
    d.addEventListener("auxclick", countClick, true);
    function open(e) {
      var trigger = e.target.closest && e.target.closest("[data-details]");
      if (!trigger) {
        return;
      }
      e.preventDefault();
      e.stopPropagation();
      if (!trigger.closest("#detail")) {
        editorPlace = placeOf(trigger);
      }
      openDetail(trigger.getAttribute("data-details"));
    }
    d.addEventListener("click", open, true);
    d.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        open(e);
      }
    }, true);

    d.addEventListener("click", function (e) {
      if (!e.target.closest || !e.target.closest("#detail")) {
        return;
      }
      var act = e.target.closest("[data-detail-do]");
      if (act) {
        doDetail(act);
        return;
      }
      var tab = e.target.closest("[data-detail-tabs] > [role=tab]");
      if (tab) {
        // Tabs: the n-th tab shows the n-th panel after the tab row.
        var tabs = [].slice.call(tab.parentNode.children);
        var panels = [].slice.call(tab.parentNode.parentNode.querySelectorAll(":scope > [data-detail-panel]"));
        tabs.forEach(function (t, i) {
          t.setAttribute("aria-selected", t === tab ? "true" : "false");
          if (panels[i]) {
            panels[i].hidden = t !== tab;
          }
        });
        return;
      }
      var entry = e.target.closest("[data-pick] > button");
      if (entry) {
        pick(entry);
        return;
      }
      if (e.target.closest("[data-detail-close]")) {
        if (leaveOK(detailDialog())) {
          detailDialog().close();
        }
        return;
      }
      var check = e.target.closest("[data-detail-refresh]");
      if (!check) {
        return;
      }
      // A forced fetch of the tile (at most once a minute), then the dialog anew.
      check.disabled = true;
      fetch(check.getAttribute("data-detail-refresh"), { credentials: "same-origin" })
        .then(function () { openDetail(detailURL); });
    });
  }

  // ── Kimai Lite add form: offer only the chosen project's and global activities ──
  function filterActivities(form) {
    var project = form.querySelector("[data-kimai-project]");
    var activity = form.querySelector("[data-kimai-activity]");
    if (!project || !activity) {
      return;
    }
    var firstShown = null;
    [].forEach.call(activity.options, function (o) {
      var owner = o.getAttribute("data-project");
      o.hidden = owner !== "" && owner !== project.value;
      if (!o.hidden && !firstShown) {
        firstShown = o;
      }
    });
    if (activity.selectedOptions.length && activity.selectedOptions[0].hidden && firstShown) {
      firstShown.selected = true;
    }
  }

  // Narrow the project list to names containing the search text ("acme
  // web" matches "Acme · Website"); keep a visible project selected.
  function searchProjects(input) {
    var project = input.form && input.form.querySelector("[data-kimai-project]");
    if (!project) {
      return;
    }
    var words = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    var firstShown = null;
    [].forEach.call(project.options, function (o) {
      var text = o.textContent.toLowerCase();
      o.hidden = !words.every(function (w) { return text.indexOf(w) >= 0; });
      if (!o.hidden && !firstShown) {
        firstShown = o;
      }
    });
    if (project.selectedOptions.length && project.selectedOptions[0].hidden && firstShown) {
      firstShown.selected = true;
      filterActivities(input.form);
    }
  }

  function setupKimaiForm() {
    d.addEventListener("change", function (e) {
      if (e.target.matches && e.target.matches("[data-kimai-project]")) {
        filterActivities(e.target.form);
      }
    });
    d.addEventListener("input", function (e) {
      if (e.target.matches && e.target.matches("[data-kimai-search]")) {
        searchProjects(e.target);
      }
    });
    // Enter in the search picks, it does not save the form.
    d.addEventListener("keydown", function (e) {
      if (e.key === "Enter" && e.target.matches && e.target.matches("[data-kimai-search]")) {
        e.preventDefault();
      }
    });
    d.addEventListener("htmx:afterSettle", function () {
      [].forEach.call(d.querySelectorAll("form.kl-new"), filterActivities);
    });
  }

  // A new connection's login follows the space until chosen by hand:
  // fixed in the personal space, a template elsewhere (startsFixed).
  function setupConnMode() {
    d.addEventListener("change", function (e) {
      var t = e.target;
      if (t.id === "mode") {
        t.dataset.chosen = "1";
      }
      if (t.id !== "space_id" || !t.form) {
        return;
      }
      var mode = t.form.querySelector("#mode");
      if (!mode || mode.dataset.chosen || t.form.dataset.fixedAlways) {
        return;
      }
      mode.value = t.value === t.dataset.personal ? "shared" : "personal";
    });
  }

  // ── Wall display: fullscreen on first tap, rotate boards, dim at night ──
  var KIOSK_DIM_CHECK_MS = 60000;
  // kioskTimers survive boosted page changes, which run setupKiosk again:
  // cleared first, so rotation and dimming never pile up.
  var kioskTimers = { next: 0, dim: 0 };

  function inDim(spec, hour) {
    var parts = spec.split("-");
    var from = +parts[0], to = +parts[1];
    return from <= to ? hour >= from && hour < to : hour >= from || hour < to;
  }

  function setupKiosk() {
    var body = d.body;
    window.clearTimeout(kioskTimers.next);
    window.clearInterval(kioskTimers.dim);
    if (!body.classList.contains("is-kiosk")) {
      return;
    }
    d.addEventListener("click", function () {
      if (!d.fullscreenElement && d.documentElement.requestFullscreen) {
        d.documentElement.requestFullscreen().catch(function () {});
      }
    }, { once: true });

    var next = body.dataset.kioskNext, every = +body.dataset.kioskEvery;
    if (next && every > 0) {
      kioskTimers.next = window.setTimeout(function () { window.location.href = next; }, every * 1000);
    }

    var dim = body.dataset.kioskDim;
    if (!dim) {
      return;
    }
    var check = function () { body.classList.toggle("is-dim", inDim(dim, new Date().getHours())); };
    check();
    kioskTimers.dim = window.setInterval(check, KIOSK_DIM_CHECK_MS);
  }

  // ── Offline view: service worker keeps the last state, banner says so ──
  function setupOffline() {
    if ("serviceWorker" in navigator) {
      navigator.serviceWorker.register("/sw.js").catch(function () {});
    }
    var show = function (hidden) {
      var note = d.querySelector(".offline-note");
      if (note) {
        note.hidden = hidden;
      }
    };
    window.addEventListener("online", function () { show(true); });
    window.addEventListener("offline", function () { show(false); });
  }

  // offlineNote puts the banner into the current page, hidden while online.
  function offlineNote() {
    var meta = d.querySelector('meta[name="offline-note"]');
    if (!meta || d.querySelector(".offline-note")) {
      return;
    }
    var note = d.createElement("p");
    note.className = "offline-note";
    note.textContent = meta.content;
    note.hidden = navigator.onLine;
    d.body.prepend(note);
  }

  // Tiles without stored data yet ask again shortly: the first fetch is
  // already running in the background.
  var RETRY_MS = 15000;

  function retryPending(root) {
    [].forEach.call(root.querySelectorAll("[data-pending]"), function (note) {
      var body = note.closest(".card-body[hx-get]");
      if (!body || body.hasAttribute("data-retrying") || typeof htmx === "undefined") {
        return;
      }
      body.setAttribute("data-retrying", "");
      window.setTimeout(function () {
        body.removeAttribute("data-retrying");
        htmx.trigger(body, "retry");
      }, RETRY_MS);
    });
  }

  function setupRetry() {
    d.addEventListener("htmx:afterSwap", function (e) { retryPending(e.target); });
  }

  // ── Place search in setup forms: a pick fills name and coordinates ──
  function setupPlacePick() {
    d.addEventListener("click", function (e) {
      var hit = e.target.closest && e.target.closest(".place-hit");
      if (!hit) {
        return;
      }
      var box = hit.closest(".place-pick");
      var set = function (field, value) { box.querySelector('[data-place-field="' + field + '"]').value = value; };
      set("place", hit.getAttribute("data-place"));
      set("lat", hit.getAttribute("data-lat"));
      set("lon", hit.getAttribute("data-lon"));
      box.querySelector('input[type="search"]').value = hit.getAttribute("data-place");
      box.querySelector("[data-place-coords]").textContent = hit.getAttribute("data-lat") + ", " + hit.getAttribute("data-lon");
      box.querySelector(".place-results").innerHTML = "";
      // Tell the form (live preview) that the place changed.
      box.querySelector('[data-place-field="place"]').dispatchEvent(new Event("input", { bubbles: true }));
    });
  }

  // ── Soft page changes: htmx swaps the body (hx-boost), this syncs the rest ──
  //
  //   click ─► GET via htmx ─► beforeSwap: non-HTML (download) or kiosk page
  //                             → normal page load instead
  //                           ─► head: metas, lang, new stylesheets (early)
  //                           ─► body swapped ─► afterSwap: new scripts,
  //                              stale stylesheets out ─► afterSettle: page setup
  var pageFns = [];
  var page = 0;
  var nextHead = null;

  function runPage(fn) {
    if (fn.andonPage === page) {
      return;
    }
    fn.andonPage = page;
    fn();
  }

  // andonPage registers a page setup: it runs on the first load and after
  // every soft page change, once per page.
  window.andonPage = function (fn) {
    pageFns.push(fn);
    if (d.readyState === "loading") {
      d.addEventListener("DOMContentLoaded", function () { runPage(fn); });
      return;
    }
    runPage(fn);
  };

  function htmlAttr(el, name) {
    return el.getAttribute(name) || "";
  }

  // headOf parses a page up to its <body> tag: all syncHead reads. htmx
  // parses the whole answer anyway, half a megabyte on a big board.
  function headOf(html) {
    var body = /<body[^>]*>/i.exec(html);
    var upTo = body ? html.slice(0, body.index + body[0].length) : html;
    return new DOMParser().parseFromString(upTo, "text/html");
  }

  // syncHead takes over what the new page declares in <head> and on <body>.
  function syncHead(next) {
    d.documentElement.lang = next.documentElement.lang;
    [].forEach.call(next.head.querySelectorAll("meta[name]"), function (meta) {
      var own = d.head.querySelector('meta[name="' + meta.name + '"]');
      if (own) {
        own.content = meta.content;
        return;
      }
      d.head.appendChild(meta.cloneNode());
    });
    [].forEach.call(next.head.querySelectorAll('link[rel="stylesheet"]'), function (link) {
      if (!d.head.querySelector('link[rel="stylesheet"][href="' + htmlAttr(link, "href") + '"]')) {
        d.head.appendChild(link.cloneNode());
      }
    });
    [].slice.call(d.body.attributes).forEach(function (a) {
      if (a.name !== "hx-boost") {
        d.body.removeAttribute(a.name);
      }
    });
    [].forEach.call(next.body.attributes, function (a) { d.body.setAttribute(a.name, a.value); });
  }

  // settleHead drops stylesheets the new page no longer links and loads its
  // new scripts in order.
  function settleHead(next) {
    var want = [].map.call(next.head.querySelectorAll('link[rel="stylesheet"]'), function (l) { return htmlAttr(l, "href"); });
    [].forEach.call(d.head.querySelectorAll('link[rel="stylesheet"]'), function (link) {
      if (want.indexOf(htmlAttr(link, "href")) < 0) {
        link.remove();
      }
    });
    [].forEach.call(next.head.querySelectorAll("script[src]"), function (script) {
      if (d.head.querySelector('script[src="' + htmlAttr(script, "src") + '"]')) {
        return;
      }
      var s = d.createElement("script");
      s.src = htmlAttr(script, "src");
      s.async = false;
      d.head.appendChild(s);
    });
  }

  function setupBoost() {
    if (typeof htmx === "undefined") {
      return;
    }
    d.addEventListener("htmx:beforeSwap", function (e) {
      if (!e.detail.boosted) {
        return;
      }
      // A section answer (a form targeting its section, see boardPart) is
      // no page change; on an error (a stale version) reload the page.
      if (e.detail.target !== d.body) {
        forgetPlace();
        if (e.detail.xhr.status >= 400) {
          e.detail.shouldSwap = false;
          window.location.reload();
        }
        return;
      }
      var xhr = e.detail.xhr;
      var type = xhr.getResponseHeader("Content-Type") || "";
      if (type.indexOf("text/html") !== 0) {
        e.detail.shouldSwap = false;
        if (e.detail.requestConfig.verb === "get") {
          window.location.href = xhr.responseURL;
          return;
        }
        var pre = d.createElement("pre");
        pre.textContent = xhr.responseText;
        e.detail.serverResponse = "<body>" + pre.outerHTML + "</body>";
        e.detail.shouldSwap = true;
        e.detail.isError = false;
        return;
      }

      var next = headOf(xhr.responseText);
      if (next.body.classList.contains("is-kiosk")) {
        e.detail.shouldSwap = false;
        window.location.href = xhr.responseURL;
        return;
      }
      // Forms answer errors with a status (e.g. 422 plus the form again): show them.
      e.detail.shouldSwap = true;
      e.detail.isError = false;
      nextHead = next;
      syncHead(next);
    });
    d.addEventListener("htmx:afterSwap", function (e) {
      if (!nextHead || !e.detail.boosted) {
        return;
      }
      page += 1;
      settleHead(nextHead);
    });
    d.addEventListener("htmx:afterSettle", function (e) {
      if (!nextHead || !e.detail.boosted) {
        return;
      }
      nextHead = null;
      pageFns.forEach(runPage);
    });
  }

  // applyStyles turns data-style="--p:72%" into the element's style. The
  // CSP forbids style attributes in markup; setting them through the
  // CSSOM is allowed. Values come from services (a Kimai project color),
  // so only known properties and plain values pass: no url(), no ";".
  var STYLE_PROP = /^(--[a-z][a-z-]*|width|left|top|color|background)$/;
  var STYLE_VALUE = /^(-?[\d.]+(%|deg|rem|px|s)?|#[0-9a-fA-F]{3,8}|var\(--[a-z0-9-]+\)|[a-z]+)$/;

  // mountMaps draws a dialog's maps (Kante's kante-map.js).
  function mountMaps(root) {
    if (window.Kante && window.Kante.map) {
      window.Kante.map.mount(root);
    }
  }

  function applyStyles(root) {
    var els = [].slice.call(root.querySelectorAll ? root.querySelectorAll("[data-style]") : []);
    if (root.hasAttribute && root.hasAttribute("data-style")) {
      els.push(root);
    }
    els.forEach(function (el) {
      el.getAttribute("data-style").split(";").forEach(function (decl) {
        var at = decl.indexOf(":");
        var prop = decl.slice(0, at).trim();
        var value = decl.slice(at + 1).trim();
        if (at < 0 || !STYLE_PROP.test(prop) || !STYLE_VALUE.test(value)) {
          return;
        }
        el.style.setProperty(prop, value);
      });
      el.removeAttribute("data-style");
    });
  }
  applyStyles(d);
  d.addEventListener("htmx:load", function (e) { applyStyles(e.target); });

  // ── Live data: Kante's motion for changing values (window.Kante) ──
  //
  //   htmx swaps a tile's fragment ─► its nodes are replaced, so a changed
  //   value shows only when the [data-live] texts before and after are compared
  //     L1  value changed   Kante.tick counts the number up, a cyan strip fades
  //     L2  fresh data      Kante.fresh: a line runs along the tile's bottom edge
  //     L3  old data        Kante.stale: the tile dims, warning stripes and the
  //                         age sit on the bottom edge (the fragment marks it
  //                         with data-stale="<age>")
  //   Without Kante's script the page works the same, only without the motion.
  var liveBefore = new WeakMap();

  d.addEventListener("htmx:beforeSwap", function (e) {
    var target = e.detail.target;
    if (!window.Kante || e.detail.boosted || !target || !target.querySelectorAll) {
      return;
    }
    liveBefore.set(target, [].map.call(target.querySelectorAll("[data-live]"), function (el) { return el.textContent; }));
  });

  d.addEventListener("htmx:afterSwap", function (e) {
    var target = e.detail.target;
    var before = target && liveBefore.get(target);
    if (!window.Kante || !before) {
      return;
    }
    liveBefore.delete(target);
    [].forEach.call(target.querySelectorAll("[data-live]"), function (el, i) {
      var next = el.textContent;
      if (before[i] === undefined || before[i] === next) {
        return;
      }
      el.textContent = before[i];
      window.Kante.tick(el, next);
    });
  });

  // syncTile shows a tile as stale (its fragment carries data-stale) or as
  // fresh (a line runs once).
  function syncTile(tile, motion) {
    var note = tile.querySelector("[data-stale]");
    window.Kante.stale(tile, !!note, note ? note.getAttribute("data-stale") : undefined);
    if (!note && motion) {
      window.Kante.fresh(tile);
    }
  }

  d.addEventListener("htmx:afterSettle", function (e) {
    if (!window.Kante || e.detail.boosted || !e.target.closest) {
      return;
    }
    var own = e.target.closest("[data-live-tile]");
    var tiles = own ? [own] : [].slice.call(e.target.querySelectorAll("[data-live-tile]"));
    tiles.forEach(function (tile) { syncTile(tile, true); });
  });

  // andonKante runs fn with Kante once its script has set window.Kante: it
  // does that on DOMContentLoaded, after the deferred scripts ran. Nothing
  // happens when the script is missing.
  window.andonKante = function (fn) {
    if (window.Kante) {
      fn(window.Kante);
      return;
    }
    d.addEventListener("DOMContentLoaded", function () {
      if (window.Kante) {
        fn(window.Kante);
      }
    });
  };

  // Fragments rendered with the page can be stale from the start.
  window.andonPage(function () {
    window.andonKante(function () {
      [].forEach.call(d.querySelectorAll("[data-live-tile]:has([data-stale])"), function (tile) { syncTile(tile, false); });
    });
  });

  // Times Andon formats itself are in the server's zone. Where the
  // browser's zone differs, their .tz-mark names it: "since 12:26 UTC".
  function showZones(root) {
    var here = -new Date().getTimezoneOffset();
    [].forEach.call(root.querySelectorAll(".tz-mark"), function (mark) {
      mark.hidden = parseInt(mark.getAttribute("data-offset"), 10) === here;
    });
  }
  window.andonPage(function () { showZones(d); });
  d.addEventListener("htmx:load", function (e) { showZones(e.target); });

  // A toast (Kante .toast) stays as long as its life line runs (--life on
  // .toast-life, 4 s by default), then goes.
  var TOAST_LIFE_MS = 4000;
  d.addEventListener("htmx:load", function (e) {
    var toast = e.target;
    if (!toast.classList || !toast.classList.contains("toast")) {
      return;
    }
    var line = toast.querySelector(".toast-life");
    var life = line ? parseFloat(getComputedStyle(line).getPropertyValue("--life")) : NaN;
    setTimeout(function () { toast.remove(); }, life ? life * 1000 : TOAST_LIFE_MS);
  });

  // Tiles poll ("every 300s") only while the tab is visible; a poll missed
  // in the background runs once when the tab shows again ("wake"). A wall
  // display keeps polling. Filters in hx-trigger would need eval, which
  // the CSP forbids, hence the events.
  // A tile's own refresh waits while someone works in it: an open form
  // ([data-hold], Kimai Lite's add and day forms), a focused field or one
  // with something typed. The next poll tries again.
  d.addEventListener("htmx:beforeRequest", function (e) {
    var el = e.detail.elt;
    if (el.classList && el.classList.contains("card-body") && busy(el)) {
      e.preventDefault();
    }
  });
  function busy(body) {
    if (body.querySelector("[data-hold]") || body.contains(d.activeElement)) {
      return true;
    }
    return [].some.call(body.querySelectorAll("input:not([type=hidden]), textarea"), function (f) {
      return f.value !== f.defaultValue;
    });
  }
  d.addEventListener("htmx:beforeRequest", function (e) {
    var el = e.detail.elt;
    if (!d.hidden || d.body.classList.contains("is-kiosk")) {
      return;
    }
    if ((el.getAttribute("hx-trigger") || "").indexOf("wake") < 0) {
      return;
    }
    e.preventDefault();
    el.setAttribute("data-missed", "");
  });
  d.addEventListener("visibilitychange", function () {
    if (d.hidden) {
      return;
    }
    [].forEach.call(d.querySelectorAll("[data-missed]"), function (el) {
      el.removeAttribute("data-missed");
      htmx.trigger(el, "wake");
    });
  });

  // Soft page changes for every same-origin link and form, except on a wall
  // display, which rotates by full page loads.
  if (!d.body.classList.contains("is-kiosk")) {
    d.body.setAttribute("hx-boost", "true");
  }

  // Space settings: the rule search shows matching rules and opens their
  // groups; a link to #rule-<id> opens that rule's group.
  function setupRuleSearch() {
    var q = d.getElementById("rule-q");
    if (!q) {
      return;
    }
    var target = location.hash && d.getElementById(location.hash.slice(1));
    if (target && target.closest("[data-rule-group]")) {
      target.closest("[data-rule-group]").open = true;
      target.scrollIntoView();
    }
    q.addEventListener("input", function () {
      var text = q.value.trim().toLowerCase();
      var any = false;
      [].forEach.call(d.querySelectorAll("[data-rule-group]"), function (group) {
        var shown = 0;
        [].forEach.call(group.querySelectorAll("[data-q]"), function (row) {
          var hit = !text || row.getAttribute("data-q").toLowerCase().indexOf(text) >= 0;
          row.hidden = !hit;
          shown += hit ? 1 : 0;
        });
        group.hidden = shown === 0;
        group.open = !!text && shown > 0;
        any = any || shown > 0;
      });
      d.querySelector(".rule-none").hidden = any;
    });
  }

  d.addEventListener("DOMContentLoaded", function () {
    setupRetry();
    setupRuleSearch();
    setupAutosubmit();
    setupMenus();
    setupHintPop();
    setupDetail();
    setupEditor();
    setupIconUpload();
    setupKimaiForm();
    setupConnMode();
    setupOffline();
    setupHotkeys();
    setupReceiptKeys();
    setupHintKeys();
    setupFolding();
    setupContextMenu();
    setupPalette();
    setupClicks();
    setupConfirm();
    setupPlacePick();
    setupBoost();
    setupPlace();
    window.setInterval(tick, CLOCK_TICK_MS);
  });

  // setupBoardOrder: board cards sort by their grip; the new order is
  // saved at once (POST /boards/order, one id per board), then the header
  // navigation and the cards (start tag, move items) come back from the
  // server. Sortable comes with the page and may still load after a soft
  // page change.
  var boardOrderSeq = 0;

  function refreshBoards(grid) {
    var seq = ++boardOrderSeq;
    return fetch("/boards", { credentials: "same-origin" }).then(function (resp) {
      return resp.ok ? resp.text() : Promise.reject(resp.status);
    }).then(function (html) {
      // A later drag wins: its answer is the newer one.
      if (seq !== boardOrderSeq) {
        return;
      }
      var next = new DOMParser().parseFromString(html, "text/html");
      var nav = d.querySelector(".app-links");
      var nextNav = next.querySelector(".app-links");
      var nextGrid = next.querySelector("[data-board-order]");
      if (nav && nextNav) {
        nav.innerHTML = nextNav.innerHTML;
      }
      if (nextGrid) {
        grid.innerHTML = nextGrid.innerHTML;
        applyStyles(grid);
      }
    });
  }

  function setupBoardOrder() {
    var grid = d.querySelector("[data-board-order]");
    if (!grid) {
      return;
    }
    if (typeof Sortable === "undefined") {
      var script = d.querySelector("script[src*=\"Sortable\"]");
      if (script) {
        script.addEventListener("load", setupBoardOrder, { once: true });
      }
      return;
    }
    if (Sortable.get(grid)) {
      return;
    }
    Sortable.create(grid, {
      handle: ".grip",
      draggable: ".board-card",
      animation: 150,
      onEnd: function (e) {
        if (e.oldIndex === e.newIndex) {
          return;
        }
        var ids = new URLSearchParams();
        [].forEach.call(grid.querySelectorAll("[data-board]"), function (card) {
          ids.append("id", card.getAttribute("data-board"));
        });
        post("/boards/order", ids).then(function (resp) {
          if (resp.ok) {
            return refreshBoards(grid);
          }
          window.location.reload();
        });
      }
    });
  }

  window.andonPage(function () {
    setupBoardOrder();
    openEditorParam();
    restorePlace();
    retryPending(d);
    setupSearch();
    bindPalette();
    offlineNote();
    setupKiosk();
    tick();
  });
})();
