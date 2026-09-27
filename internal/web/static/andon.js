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
      if (q) {
        sec.classList.remove("is-collapsed");
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

  function setupSearch() {
    var input = d.getElementById("search");
    if (!input) {
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
    [].forEach.call(d.querySelectorAll(".clock"), function (el) {
      var zone = el.getAttribute("data-tz");
      var locale = el.getAttribute("data-locale") || undefined;
      var seconds = el.getAttribute("data-seconds") === "yes";
      var now = new Date();
      var time = { hour: "2-digit", minute: "2-digit", timeZone: zone };
      if (seconds) {
        time.second = "2-digit";
      }
      if (el.getAttribute("data-h12") === "yes") {
        time.hour = "numeric";
        time.hour12 = true;
      }
      var face = el.querySelector(".clock-face");
      if (face) {
        try {
          var hms = now.toLocaleTimeString("en-GB", { hour12: false, timeZone: zone }).split(":").map(Number);
          var m = hms[1] + hms[2] / 60;
          face.querySelector(".hand-h").style.setProperty("--a", ((hms[0] % 12) * 30 + m / 2) + "deg");
          face.querySelector(".hand-m").style.setProperty("--a", (m * 6) + "deg");
          var sec = face.querySelector(".hand-s");
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

  // ── Folding: instant in the page, stored in the personal overlay ──
  function setupFolding() {
    d.addEventListener("click", function (e) {
      var btn = e.target.closest && e.target.closest(".fold-btn");
      if (!btn) {
        return;
      }

      var sec = btn.closest(".dsec");
      var closed = sec.classList.toggle("is-collapsed");
      btn.setAttribute("aria-expanded", String(!closed));
      post(btn.getAttribute("data-fold"), { state: closed ? "closed" : "open" });
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

  // ── Selects that submit their form on change (no inline handlers: CSP) ──
  function setupAutosubmit() {
    d.addEventListener("change", function (e) {
      var el = e.target.closest && e.target.closest("[data-autosubmit]");
      if (el && el.form) {
        el.form.requestSubmit();
      }
    });
  }

  // ── Header menus (<details>): one open at a time, closed by outside click or Esc ──
  function setupMenus() {
    d.addEventListener("click", function (e) {
      d.querySelectorAll("details.menu[open]").forEach(function (m) {
        if (!m.contains(e.target)) {
          m.open = false;
        }
      });
    });
    d.addEventListener("keydown", function (e) {
      if (e.key === "Escape") {
        d.querySelectorAll("details.menu[open]").forEach(function (m) { m.open = false; });
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

  function setupKimaiForm() {
    d.addEventListener("change", function (e) {
      if (e.target.matches && e.target.matches("[data-kimai-project]")) {
        filterActivities(e.target.form);
      }
    });
    d.addEventListener("htmx:afterSettle", function () {
      [].forEach.call(d.querySelectorAll("form.kl-new"), filterActivities);
    });
  }

  // ── Wall display: fullscreen on first tap, rotate boards, dim at night ──
  var KIOSK_DIM_CHECK_MS = 60000;

  function inDim(spec, hour) {
    var parts = spec.split("-");
    var from = +parts[0], to = +parts[1];
    return from <= to ? hour >= from && hour < to : hour >= from || hour < to;
  }

  function setupKiosk() {
    var body = d.body;
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
      window.setTimeout(function () { window.location.href = next; }, every * 1000);
    }

    var dim = body.dataset.kioskDim;
    if (!dim) {
      return;
    }
    var check = function () { body.classList.toggle("is-dim", inDim(dim, new Date().getHours())); };
    check();
    window.setInterval(check, KIOSK_DIM_CHECK_MS);
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

      var next = new DOMParser().parseFromString(xhr.responseText, "text/html");
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
  var STYLE_PROP = /^(--[a-z]+|width|left|top|background)$/;
  var STYLE_VALUE = /^(-?[\d.]+(%|deg|rem|px)?|#[0-9a-fA-F]{3,8}|var\(--[a-z0-9-]+\)|[a-z]+)$/;

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

  // Soft page changes for every same-origin link and form, except on a wall
  // display, which rotates by full page loads.
  if (!d.body.classList.contains("is-kiosk")) {
    d.body.setAttribute("hx-boost", "true");
  }

  d.addEventListener("DOMContentLoaded", function () {
    setupRetry();
    setupAutosubmit();
    setupMenus();
    setupHintPop();
    setupKimaiForm();
    setupOffline();
    setupHotkeys();
    setupReceiptKeys();
    setupFolding();
    setupContextMenu();
    setupPalette();
    setupClicks();
    setupConfirm();
    setupPlacePick();
    setupBoost();
    window.setInterval(tick, CLOCK_TICK_MS);
  });

  window.andonPage(function () {
    retryPending(d);
    setupSearch();
    bindPalette();
    offlineNote();
    setupKiosk();
    tick();
  });
})();
