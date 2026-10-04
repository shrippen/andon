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
    var kind = FILTERS[pressed ? pressed.getAttribute("data-filter") : "all"];
    var any = false;
    [].forEach.call(root.querySelectorAll(".gal-group"), function (group) {
      var shown = 0;
      [].forEach.call(group.querySelectorAll("[data-q]"), function (card) {
        var hit = (!q || (card.getAttribute("data-q") || "").toLowerCase().indexOf(q) >= 0) &&
          (!mine || card.hasAttribute("data-mine")) && (!kind || kind(card));
        card.hidden = !hit;
        shown += hit ? 1 : 0;
      });
      group.hidden = shown === 0;
      any = any || shown > 0;
    });
    root.querySelector(".gal-none").hidden = any;
  }

  // Delegated: the gallery arrives in the detail dialog after page load.
  function refilter(e) {
    if (e.target.id !== "gal-q" && e.target.id !== "gal-mine") {
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

  // Closing the side panel empties it; CSS hides it when empty.
  d.addEventListener("click", function (e) {
    if (!e.target.closest || !e.target.closest("[data-side-close]")) {
      return;
    }
    var side = d.getElementById("gal-side");
    side.innerHTML = "";
    [].forEach.call(d.querySelectorAll(".gal-card[aria-current]"), function (c) { c.removeAttribute("aria-current"); });
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
