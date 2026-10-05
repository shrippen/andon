/* Gallery (in the detail dialog) and library: filter cards by search text, "only my connections"
   and tiles set up, open a card's side panel, pick unused tiles to delete, open the reuse dialog
   of a set-up tile, fill a library row's menu. */
(function () {
  "use strict";

  var d = document;

  // FILTERS test a card for the pressed filter button (data-filter):
  // "set" = has tiles, "new" = none yet, "unused" = some on no board.
  var FILTERS = {
    all: function () { return true; },
    set: function (card) { return card.hasAttribute("data-tiles"); },
    "new": function (card) { return !card.hasAttribute("data-tiles"); },
    unused: function (card) { return card.hasAttribute("data-unused"); }
  };

  // filter hides cards that don't match, then groups left empty; root is
  // the gallery dialog or the library page.
  function filter(root) {
    var q = root.querySelector("#gal-q").value.trim().toLowerCase();
    var box = root.querySelector("#gal-mine");
    var mine = box ? box.checked : false;
    var pressed = root.querySelector("#gal-filter [aria-pressed=\"true\"]");
    var name = pressed ? pressed.getAttribute("data-filter") : "all";
    // In the library a filter button names a space kind (data-space on the row).
    var kind = FILTERS[name] || function (row) { return row.getAttribute("data-space") === name; };
    // Library flag chips: a row needs every checked flag (data-unnamed, data-unused).
    var flags = [].map.call(root.querySelectorAll("#lib-flags input:checked"), function (box) {
      return "data-" + box.getAttribute("data-flag");
    });
    var narrowed = q || flags.length > 0 || name !== "all";
    var any = false;
    [].forEach.call(root.querySelectorAll(".gal-group"), function (group) {
      var shown = 0;
      [].forEach.call(group.querySelectorAll("[data-q]"), function (card) {
        var hit = (!q || (card.getAttribute("data-q") || "").toLowerCase().indexOf(q) >= 0) &&
          (!mine || card.hasAttribute("data-mine")) && (!kind || kind(card)) &&
          flags.every(function (flag) { return card.hasAttribute(flag); });
        card.hidden = !hit;
        shown += hit ? 1 : 0;
      });
      group.hidden = shown === 0;
      any = any || shown > 0;
      // Counters follow the filter: in the group's heading and in the topic list.
      var count = group.querySelector(".hint-group-count, summary > .count");
      if (count) {
        count.textContent = shown;
      }
      var link = group.id && root.querySelector(".gal-nav a[href=\"#" + group.id + "\"] span");
      if (link) {
        link.textContent = shown;
      }
      // A folded group (the links) opens when a search or filter finds rows in it.
      if (group.tagName === "DETAILS" && narrowed && shown > 0) {
        group.open = true;
      }
    });
    root.querySelector(".gal-none").hidden = any;
  }

  // Delegated: the gallery arrives in the detail dialog after page load.
  function refilter(e) {
    if (e.target.id !== "gal-q" && e.target.id !== "gal-mine" && !e.target.closest("#lib-flags")) {
      return;
    }
    filter(e.target.closest(".gallery") || d);
  }
  d.addEventListener("input", refilter);
  d.addEventListener("change", refilter);

  // A filter button: press it alone, then filter again.
  d.addEventListener("click", function (e) {
    var button = e.target.closest && e.target.closest("#gal-filter [data-filter]");
    if (!button) {
      return;
    }
    [].forEach.call(button.parentNode.children, function (b) {
      b.setAttribute("aria-pressed", b === button ? "true" : "false");
    });
    filter(button.closest(".gallery") || d);
  });

  // A click anywhere on a card opens its side panel (the title button
  // carries hx-get); links and buttons inside keep their own action.
  d.addEventListener("click", function (e) {
    var card = e.target.closest && e.target.closest(".gal-card");
    if (!card) {
      return;
    }
    var open = card.querySelector(".gal-open");
    if (!e.target.closest("a, button, input, label")) {
      open.click();
      return;
    }
    if (e.target.closest(".gal-open")) {
      [].forEach.call(d.querySelectorAll(".gal-card[aria-current]"), function (c) { c.removeAttribute("aria-current"); });
      card.setAttribute("aria-current", "true");
    }
  });

  var LEAVE_MAX_MS = 1000;

  // closeSide slides the side panel out (Kante's .is-leaving), then
  // empties it; CSS hides it when empty. Without motion at once.
  function closeSide() {
    var side = d.getElementById("gal-side");
    if (!side || !side.firstChild || side.classList.contains("is-leaving")) {
      return;
    }
    [].forEach.call(d.querySelectorAll(".gal-card[aria-current]"), function (c) { c.removeAttribute("aria-current"); });
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      side.innerHTML = "";
      return;
    }
    side.classList.add("is-leaving");
    // Emptied when the slide ends, at the latest after LEAVE_MAX_MS: a
    // browser may pause animations of a window in the background.
    var timer = window.setTimeout(done, LEAVE_MAX_MS);
    function done(e) {
      if (e && e.target !== side) {
        return;
      }
      window.clearTimeout(timer);
      side.removeEventListener("animationend", done);
      side.classList.remove("is-leaving");
      side.innerHTML = "";
    }
    side.addEventListener("animationend", done);
  }

  // While the panel is open, a click elsewhere in the gallery dialog only
  // closes it (like a click on a scrim); its own button too. Clicks in the
  // dialogs opened from the panel (reuse, confirm) don't count. On window,
  // capturing: before the document's handlers (htmx, data-details) see it.
  window.addEventListener("click", function (e) {
    var side = d.getElementById("gal-side");
    if (!side || !side.firstChild || !e.target.closest) {
      return;
    }
    if (e.target.closest("[data-side-close]")) {
      closeSide();
      return;
    }
    var dialog = e.target.closest("dialog");
    if (!dialog || dialog.id !== "detail" || side.contains(e.target)) {
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    closeSide();
  }, true);

  // Escape closes the panel first, the dialog only after.
  d.addEventListener("keydown", function (e) {
    var side = d.getElementById("gal-side");
    if (e.key !== "Escape" || !side || !side.firstChild || d.querySelector("#detail dialog[open]")) {
      return;
    }
    e.preventDefault();
    closeSide();
  });

  // Picking unused tiles in the side panel: "all" toggles every box, the
  // bar shows the count and the delete button while any is picked.
  d.addEventListener("change", function (e) {
    var list = e.target.closest && e.target.closest("#gal-side-list");
    if (!list || e.target.type !== "checkbox") {
      return;
    }
    var boxes = list.querySelectorAll('input[name="ids"]');
    if (e.target.hasAttribute("data-pick-all")) {
      [].forEach.call(boxes, function (b) { b.checked = e.target.checked; });
    }
    var n = list.querySelectorAll('input[name="ids"]:checked').length;
    var bar = list.querySelector("[data-picked-bar]");
    bar.querySelector("[data-picked]").textContent = n;
    bar.hidden = n === 0;
  });

  // Registered once: this script stays loaded across soft page changes.
  d.addEventListener("click", function (e) {
    var opener = e.target.closest("[data-open]:not([data-open=\"palette\"])");
    if (!opener) {
      return;
    }
    var dialog = d.getElementById(opener.getAttribute("data-open"));
    if (!dialog || !dialog.showModal || dialog.open) {
      return;
    }
    if (opener.hasAttribute("data-reuse")) {
      fillReuse(dialog, opener);
    }
    dialog.showModal();
  });

  // fillReuse points the shared reuse dialog at the card's tile:
  // widget id into the place form and the copy action, name and kind
  // into the texts. The copy action keeps {widget} in data-action.
  function fillReuse(dialog, opener) {
    var id = opener.getAttribute("data-reuse");
    dialog.querySelector('input[name="widget_id"]').value = id;
    [].forEach.call(dialog.querySelectorAll('form[action*="{widget}"], form[data-action]'), function (form) {
      if (!form.hasAttribute("data-action")) {
        form.setAttribute("data-action", form.getAttribute("action"));
      }
      form.setAttribute("action", form.getAttribute("data-action").replace("{widget}", id));
    });
    [].forEach.call(dialog.querySelectorAll("[data-fill]"), function (el) {
      el.textContent = opener.getAttribute("data-" + el.getAttribute("data-fill")) || "";
    });
  }

  // A library row's menu, on first open: the page's one template with
  // the row's widget id in its links and form actions.
  d.addEventListener("toggle", function (e) {
    var menu = e.target;
    if (!menu.matches || !menu.matches("details.row-more[data-widget]") || !menu.open || menu.querySelector(".row-more-body")) {
      return;
    }
    var tpl = d.getElementById("row-more");
    if (!tpl) {
      return;
    }
    var id = menu.getAttribute("data-widget");
    var body = tpl.content.cloneNode(true);
    [].forEach.call(body.querySelectorAll("[href*=\"{widget}\"], [action*=\"{widget}\"]"), function (el) {
      var attr = el.hasAttribute("href") ? "href" : "action";
      el.setAttribute(attr, el.getAttribute(attr).replace("{widget}", id));
    });
    menu.appendChild(body);
  }, true);

})();
