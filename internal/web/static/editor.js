/* Drag and drop of tiles. Editors reorder the board, others their own layout. */
(function () {
  "use strict";

  var d = document;

  function csrf() {
    var meta = d.querySelector('meta[name="csrf"]');
    return meta ? meta.getAttribute("content") || "" : "";
  }

  function layout(board) {
    var result = {};
    [].forEach.call(board.querySelectorAll("[data-sortable]"), function (list) {
      result[list.getAttribute("data-sortable")] = [].map.call(
        list.querySelectorAll(":scope > .tile-slot[data-placement]"),
        function (slot) { return Number(slot.getAttribute("data-placement")); }
      );
    });
    return result;
  }

  // carryOn moves the page to the board's new version instead of reloading
  // it: the tiles already stand where they were dropped, and a board of
  // 240 tiles took seconds to reload after every drag.
  function carryOn(board, version) {
    var old = board.getAttribute("data-version");
    var inQuery = new RegExp("([?&]version=)" + old + "(?!\\d)");
    board.setAttribute("data-version", version);
    [].forEach.call(d.querySelectorAll('input[name="version"]'), function (input) {
      if (input.value === old) {
        input.value = version;
      }
    });
    [].forEach.call(d.querySelectorAll('a[href*="version="]'), function (a) {
      a.setAttribute("href", a.getAttribute("href").replace(inQuery, "$1" + version));
    });
    [].forEach.call(d.querySelectorAll("[data-board-version]"), function (el) {
      el.textContent = version;
    });

    // Tiles may have changed sections: recount.
    [].forEach.call(board.querySelectorAll("[data-sortable]"), function (list) {
      var count = list.closest(".dsec") && list.closest(".dsec").querySelector(".dsec-count");
      if (count) {
        count.textContent = list.querySelectorAll(":scope > .tile-slot[data-placement]").length;
      }
    });
  }

  function save(board) {
    var body = { version: Number(board.getAttribute("data-version")), layout: layout(board) };
    var reload = function () { window.location.reload(); };
    fetch("/boards/" + board.getAttribute("data-board") + "/arrange", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf() },
      body: JSON.stringify(body),
      credentials: "same-origin"
    }).then(function (res) {
      return res.ok ? res.json() : null;
    }).then(function (answer) {
      // Somebody else saved in between, or the save failed: reload for a
      // consistent state.
      if (!answer || !answer.version) {
        reload();
        return;
      }
      carryOn(board, String(answer.version));
    }).catch(reload);
  }

  window.andonPage(function () {
    var board = d.querySelector(".board[data-mode]");
    if (!board || !board.getAttribute("data-mode") || typeof Sortable === "undefined") {
      return;
    }

    // Editors may move tiles between sections; personal layouts only within one.
    var shared = board.getAttribute("data-mode") === "board" ? "tiles" : null;
    [].forEach.call(board.querySelectorAll("[data-sortable]"), function (list) {
      Sortable.create(list, {
        group: shared ? { name: shared } : "section-" + list.getAttribute("data-sortable"),
        animation: 120,
        draggable: ".tile-slot[data-placement]",
        filter: "[data-static], a, button, input, select, label",
        preventOnFilter: false,
        // The fallback drag lets CSS lift the tile (.sortable-drag) while a
        // dashed gap (.sortable-ghost) marks where it lands.
        forceFallback: true,
        fallbackClass: "sortable-drag",
        ghostClass: "sortable-ghost",
        onEnd: function () { save(board); }
      });
    });
  });
})();

/* Selection bar: "3 markiert", and clearing the selection. CSS shows the
   bar once a tile is checked. */
(function () {
  "use strict";

  var d = document;

  function boxes() {
    return d.querySelectorAll('input[name="placement"][form="bulk"]');
  }

  function count() {
    var label = d.querySelector("[data-sel-count]");
    if (!label) {
      return;
    }
    var n = [].filter.call(boxes(), function (b) { return b.checked; }).length;
    label.textContent = label.getAttribute("data-template").replace("{n}", n);
  }

  d.addEventListener("change", function (e) {
    if (e.target.matches && e.target.matches('input[name="placement"][form="bulk"]')) {
      count();
    }
  });
  d.addEventListener("click", function (e) {
    if (!e.target.closest || !e.target.closest("[data-sel-clear]")) {
      return;
    }
    [].forEach.call(boxes(), function (b) { b.checked = false; });
    count();
  });
})();

/* Icon upload: store the file, put the returned spec into the icon field. */
(function () {
  "use strict";

  document.addEventListener("change", function (e) {
    var input = e.target;
    if (!input.classList || !input.classList.contains("icon-upload") || !input.files.length) {
      return;
    }
    var body = new FormData();
    body.append("file", input.files[0]);
    fetch("/icons/upload", {
      method: "POST",
      headers: { "X-CSRF-Token": (document.querySelector('meta[name="csrf"]') || { content: "" }).content },
      body: body,
      credentials: "same-origin"
    }).then(function (res) { return res.text().then(function (text) { return [res.ok, text]; }); })
      .then(function (pair) {
        if (!pair[0]) {
          window.alert(pair[1]);
          return;
        }
        var field = document.querySelector('[name="' + input.getAttribute("data-target") + '"]');
        field.value = pair[1];
        field.dispatchEvent(new Event("input", { bubbles: true }));
      });
  });
})();
