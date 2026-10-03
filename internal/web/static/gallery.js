/* Gallery and library: filter cards by search text and "only my connections",
   open the reuse dialog of a set-up tile. */
(function () {
  "use strict";

  var d = document;

  // filter hides cards that don't match, then groups left empty.
  function filter() {
    var q = d.getElementById("gal-q").value.trim().toLowerCase();
    var box = d.getElementById("gal-mine");
    var mine = box ? box.checked : false;
    var any = false;
    [].forEach.call(d.querySelectorAll(".gal-group"), function (group) {
      var shown = 0;
      [].forEach.call(group.querySelectorAll("[data-q]"), function (card) {
        var hit = (!q || (card.getAttribute("data-q") || "").toLowerCase().indexOf(q) >= 0) &&
          (!mine || card.hasAttribute("data-mine"));
        card.hidden = !hit;
        shown += hit ? 1 : 0;
      });
      group.hidden = shown === 0;
      any = any || shown > 0;
    });
    d.querySelector(".gal-none").hidden = any;
  }

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

  window.andonPage(function () {
    var q = d.getElementById("gal-q");
    if (!q) {
      return;
    }
    q.addEventListener("input", filter);
    var box = d.getElementById("gal-mine");
    if (box) {
      box.addEventListener("change", filter);
    }
  });
})();
