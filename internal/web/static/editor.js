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
    var body = { version: Number(board.getAttribute("data-version")), layout: layout(board), mode: board.getAttribute("data-mode") };
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

  function editedBoard() {
    var board = d.querySelector(".board[data-mode]");
    if (!board || !board.getAttribute("data-mode") || typeof Sortable === "undefined") {
      return null;
    }
    return board;
  }

  // editMode marks a tile list as being edited: Kante turns the tier bars
  // cyan and draws four brackets on each tile, one after the other. Kante's
  // script (vendor/kante/shrippen.js) is optional: without it a board edits
  // and drags just the same.
  function editMode(list) {
    window.andonKante(function (kante) {
      [].forEach.call(list.children, function (tile) {
        if (tile.classList.contains("tile-slot")) {
          tile.setAttribute("data-editable", "");
        }
      });
      kante.edit(list, true);
    });
  }

  // The tile in hand is a copy that Sortable removes before it reports the
  // drop, so its last position is kept while the pointer moves.
  var held = null;

  function follow() {
    var copy = d.querySelector(".is-picked");
    if (copy) {
      held = copy.getBoundingClientRect();
    }
  }

  // bind makes the tile lists in root draggable, once each (a morphed
  // page keeps its lists).
  //
  //   pick up ─► the tile stays as a dashed gap (.drop-gap), a copy follows
  //              the pointer (.is-picked)
  //   drop    ─► the tile glides from where it was let go into its cell
  //              (Kante.settle)
  function bind(board, root) {
    // Editors may move tiles between sections; personal layouts only within one.
    var shared = board.getAttribute("data-mode") === "board" ? "tiles" : null;
    [].forEach.call(root.querySelectorAll("[data-sortable]"), function (list) {
      if (Sortable.get(list)) {
        return;
      }
      editMode(list);
      Sortable.create(list, {
        group: shared ? { name: shared } : "section-" + list.getAttribute("data-sortable"),
        animation: 120,
        draggable: ".tile-slot[data-placement]",
        filter: "[data-static], a, button, input, select, label",
        preventOnFilter: false,
        forceFallback: true,
        fallbackClass: "is-picked",
        ghostClass: "drop-gap",
        onStart: function () {
          held = null;
          d.addEventListener("pointermove", follow);
          d.dispatchEvent(new CustomEvent("andon:drag", { detail: true }));
        },
        onEnd: function (evt) {
          d.removeEventListener("pointermove", follow);
          d.dispatchEvent(new CustomEvent("andon:drag", { detail: false }));
          if (held && window.Kante) {
            window.Kante.settle(evt.item, held);
          }
          save(board);
        }
      });
    });
  }

  window.andonPage(function () {
    var board = editedBoard();
    if (board) {
      bind(board, board);
    }
  });

  // A section answer (see boardPart): bind its tiles, carry the board's
  // new version into the rest of the page, point at undo like ?undo does.
  d.addEventListener("htmx:load", function (e) {
    var board = editedBoard();
    if (board && e.target.matches && e.target.matches(".dsec")) {
      bind(board, e.target);
    }
  });
  d.addEventListener("boardVersion", function (e) {
    var board = editedBoard();
    if (board) {
      carryOn(board, String(e.detail.value));
    }
  });
  d.addEventListener("undoHint", function (e) {
    var undo = d.querySelector('form[action$="/undo"] button');
    if (undo && e.detail.value) {
      undo.classList.add("is-hint");
    }
  });
})();

/* The tile strip: one per page, moved into the tile at hand.
 *
 *   mouse     hover a tile        ─► strip floats over it, stays in place in
 *             the DOM (moving it into a tile made Firefox lay out the whole
 *             column flow again: ~180 ms per hover on 107 tiles)
 *   keyboard  focus a tile        ─► strip moves in, Tab reaches its tools
 *   touch     first tap on a tile ─► strip moves in and stays (is-active);
 *             the second tap opens the tile as usual
 *
 * Moving in fills the tile's values: form actions ({placement}), links
 * ({widget}), pressed states and labels, selection, "2×". A strip per
 * tile made a 240-tile board ~5,000 elements. */
(function () {
  "use strict";

  var d = document;
  var pointer = "mouse";
  var dragging = false;
  var over = null; // the tile the floating strip is over

  function strip() {
    return d.getElementById("tile-strip");
  }

  function tileOf(el) {
    return el && el.closest ? el.closest(".board[data-mode] .tile-slot[data-placement]") : null;
  }

  // toggle sets a two-state button: pressed, the value it sends, its label.
  function toggle(s, name, pressed, value, label) {
    var b = s.querySelector('[data-toggle="' + name + '"]');
    if (!b) {
      return;
    }
    b.value = value;
    b.setAttribute("aria-pressed", String(pressed));
    b.setAttribute("aria-label", label);
    b.title = label;
  }

  function fill(s, tile) {
    var id = tile.getAttribute("data-placement");
    var rows = Number(tile.getAttribute("data-rows") || 1);
    var cols = Number(tile.getAttribute("data-cols") || 1);
    var hidden = tile.classList.contains("is-hidden");

    [].forEach.call(s.querySelectorAll("[data-act]"), function (b) {
      b.setAttribute("formaction", b.getAttribute("data-act").replace("{placement}", id));
    });
    // Boosted links keep the href htmx saw first: process them again.
    [].forEach.call(s.querySelectorAll("[data-href]"), function (a) {
      a.setAttribute("href", a.getAttribute("data-href").replace("{widget}", tile.getAttribute("data-widget")));
      if (typeof htmx !== "undefined") {
        htmx.process(a);
      }
    });
    toggle(s, "rows", rows > 1, rows > 1 ? "1" : "2", rows > 1 ? s.dataset.normal : s.dataset.tall);
    toggle(s, "cols", cols > 1, cols > 1 ? "1" : "2", cols > 1 ? s.dataset.narrow : s.dataset.wide);
    toggle(s, "hidden", hidden, hidden ? "shown" : "hidden", hidden ? s.dataset.show : s.dataset.hide);

    var pick = s.querySelector("[data-pick]");
    if (pick) {
      pick.checked = picked(id);
      pick.setAttribute("aria-label", s.dataset.select + " " + (tile.getAttribute("data-title") || ""));
    }
    var repeat = s.querySelector("[data-repeat]");
    if (repeat) {
      var n = tile.getAttribute("data-placed");
      repeat.hidden = !n;
      repeat.textContent = n ? n + "×" : "";
    }
  }

  // activate moves the strip into tile; stay keeps it shown (touch).
  function activate(tile, stay) {
    var s = strip();
    if (!s || !tile) {
      return;
    }
    if (s.parentNode !== tile) {
      unfloat(s);
      fill(s, tile);
      tile.appendChild(s);
      s.hidden = false;
    }
    [].forEach.call(d.querySelectorAll(".tile-slot.is-active"), function (t) {
      if (t !== tile) {
        t.classList.remove("is-active");
      }
    });
    tile.classList.toggle("is-active", !!stay);
  }

  // unfloat drops what hover set: position, visibility, htmx target.
  function unfloat(s) {
    over = null;
    s.classList.remove("is-floating");
    s.style.top = "";
    s.style.left = "";
    s.style.width = "";
    s.style.right = "";
    s.setAttribute("hx-target", "closest .dsec");
  }

  // hover shows the strip over tile without touching the tile's subtree.
  //
  //   board-main (column flow)      strip (outside the flow)
  //   ┌──────┬──────┬──────┐          ┌────────┐
  //   │ tile │ tile │ tile │   ◄──────│ tools  │ top/left/width of the tile
  //   └──────┴──────┴──────┘          └────────┘
  function hover(tile) {
    var s = strip();
    if (!s || !tile || over === tile) {
      return;
    }
    if (tileOf(s)) {
      rest();
    }
    s.hidden = false;
    s.classList.add("is-floating");
    s.setAttribute("hx-target", "#" + tile.closest(".dsec").id);
    fill(s, tile);

    var box = tile.getBoundingClientRect();
    var parent = s.offsetParent;
    var origin = parent && parent !== d.body && parent !== d.documentElement
      ? parent.getBoundingClientRect()
      : { top: -window.scrollY, left: -window.scrollX };
    s.style.top = box.top - origin.top + "px";
    s.style.left = box.left - origin.left + "px";
    s.style.width = box.width + "px";
    s.style.right = "auto";
    over = tile;
  }

  // sink hides the floating strip again.
  function sink() {
    var s = strip();
    if (!s || !over) {
      return;
    }
    unfloat(s);
    s.hidden = true;
  }

  // rest puts the strip back next to the board, e.g. before its tile's
  // section is swapped out.
  function rest() {
    var s = strip();
    var board = d.querySelector(".board[data-mode]");
    if (!s || !board || !tileOf(s)) {
      return;
    }
    s.hidden = true;
    board.after(s);
    [].forEach.call(d.querySelectorAll(".tile-slot.is-active"), function (t) { t.classList.remove("is-active"); });
  }

  d.addEventListener("pointerdown", function (e) {
    pointer = e.pointerType || "mouse";
  }, true);
  d.addEventListener("mouseover", function (e) {
    if (pointer !== "mouse" || dragging) {
      return;
    }
    var tile = tileOf(e.target);
    if (tile) {
      hover(tile);
      return;
    }
    if (!(strip() && strip().contains(e.target))) {
      sink();
    }
  });
  d.documentElement.addEventListener("mouseleave", sink);
  d.addEventListener("focusin", function (e) {
    var tile = tileOf(e.target);
    if (tile && !dragging) {
      activate(tile, false);
    }
  });

  // Touch: the first tap on a tile shows its tools instead of opening it.
  // On window, before the page's own click handlers (click counting).
  window.addEventListener("click", function (e) {
    if (pointer !== "touch" || !strip()) {
      return;
    }
    var tile = tileOf(e.target);
    if (!tile) {
      rest();
      return;
    }
    if (tile.classList.contains("is-active") || (e.target.closest && e.target.closest("#tile-strip"))) {
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    activate(tile, true);
  }, true);

  // Before its section is swapped out, the strip rests; a keyboard user's
  // focus returns to the same control of the same tile afterwards.
  var refocus = null;
  d.addEventListener("htmx:beforeSwap", function (e) {
    if (over && e.detail.target && e.detail.target.contains(over)) {
      sink();
    }
    var s = strip();
    if (!s || !e.detail.target || e.detail.target === d.body || !e.detail.target.contains(s)) {
      return;
    }
    var focused = d.activeElement;
    refocus = s.contains(focused) ? { tile: tileOf(s).getAttribute("data-placement"), act: focused.getAttribute("data-act") } : null;
    rest();
  });
  d.addEventListener("htmx:load", function (e) {
    if (!refocus || !e.target.matches || !e.target.matches(".dsec")) {
      return;
    }
    var tile = e.target.querySelector('.tile-slot[data-placement="' + refocus.tile + '"]');
    var act = refocus.act;
    refocus = null;
    if (!tile) {
      return;
    }
    activate(tile, false);
    var control = act && strip().querySelector('[data-act="' + act + '"]');
    (control || tile).focus();
  });
  d.addEventListener("andon:drag", function (e) {
    dragging = e.detail;
    if (dragging) {
      sink();
    }
  });

  // ── Selection: picked tiles live as hidden fields in the bulk form ──

  function bulk() {
    return d.getElementById("bulk");
  }

  function field(id) {
    var form = bulk();
    return form ? form.querySelector('input[name="placement"][value="' + id + '"]') : null;
  }

  function picked(id) {
    return !!field(id);
  }

  function count() {
    var label = d.querySelector("[data-sel-count]");
    var form = bulk();
    if (!label || !form) {
      return;
    }
    var n = form.querySelectorAll('input[name="placement"]').length;
    label.textContent = label.getAttribute("data-template").replace("{n}", n);
  }

  function pick(tile, on) {
    var id = tile.getAttribute("data-placement");
    var form = bulk();
    var had = field(id);
    tile.classList.toggle("is-selected", on);
    if (on && !had && form) {
      var input = d.createElement("input");
      input.type = "hidden";
      input.name = "placement";
      input.value = id;
      form.appendChild(input);
    }
    if (!on && had) {
      had.remove();
    }
    count();
  }

  d.addEventListener("change", function (e) {
    if (e.target.matches && e.target.matches("#tile-strip [data-pick]")) {
      pick(tileOf(e.target), e.target.checked);
    }
  });
  d.addEventListener("click", function (e) {
    if (!e.target.closest || !e.target.closest("[data-sel-clear]")) {
      return;
    }
    [].forEach.call(d.querySelectorAll(".tile-slot.is-selected"), function (t) { pick(t, false); });
    [].forEach.call(d.querySelectorAll('#bulk input[name="placement"]'), function (i) { i.remove(); });
    var box = d.querySelector("#tile-strip [data-pick]");
    if (box) {
      box.checked = false;
    }
    count();
  });

  // A swapped-in section: mark its picked tiles again.
  d.addEventListener("htmx:load", function (e) {
    if (!e.target.matches || !e.target.matches(".dsec")) {
      return;
    }
    [].forEach.call(e.target.querySelectorAll(".tile-slot[data-placement]"), function (t) {
      t.classList.toggle("is-selected", picked(t.getAttribute("data-placement")));
    });
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
