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

  function save(board) {
    var body = { version: Number(board.getAttribute("data-version")), layout: layout(board) };
    fetch("/boards/" + board.getAttribute("data-board") + "/arrange", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf() },
      body: JSON.stringify(body),
      credentials: "same-origin"
    }).then(function (res) {
      // Saved, or the board changed meanwhile (409): reload for a
      // consistent state. Anything else leaves the page as it is and says so.
      if (res.ok || res.status === 409) {
        window.location.reload();
        return;
      }
      window.alert(board.getAttribute("data-save-failed") || "Saving failed (" + res.status + ").");
    }).catch(function () {
      window.alert(board.getAttribute("data-save-failed") || "Saving failed: offline?");
    });
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
