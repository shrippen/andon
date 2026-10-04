/* Gallery (in the detail dialog) and library: filter cards by search text and "only my connections",
   open the reuse dialog of a set-up tile, fill a library row's menu. */
(function () {
  "use strict";

  var d = document;

  // filter hides cards that don't match, then groups left empty; root is
  // the gallery dialog or the library page.
  function filter(root) {
    var q = root.querySelector("#gal-q").value.trim().toLowerCase();
    var box = root.querySelector("#gal-mine");
    var mine = box ? box.checked : false;
    var any = false;
    [].forEach.call(root.querySelectorAll(".gal-group"), function (group) {
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
