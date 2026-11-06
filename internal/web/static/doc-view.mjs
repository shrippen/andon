// The receipt on the VAT desk, in its own frame: pages drawn with pdf.js,
// so what the fields hold can be marked on them (a browser's PDF viewer
// marks nothing and differs per browser). An image shows as it is.
//
//   frame src  /receipts/view/201#a=342,00&d=2026-10-16&n=RE-1
//   marks      amount, day, number, found in the page's text; a scan without
//              text (no OCR) shows without marks
//
// A new hash marks again without loading the file again.

const root = document.body;
const pages = root.querySelector("[data-pages]");
const ZOOM_STEP = 1.25;
const ZOOM_MIN = 0.5;
const ZOOM_MAX = 4;
const MAX_PAGES = 30; // a receipt is short; a long file is read in the new tab
const MIN_NUMBER = 3; // shorter "numbers" are found anywhere

let zoom = 1;
let drawn = []; // {box, page, items} per page, for marking again
let lastWidth = 0;
let drawLater = false; // the file came while the frame was hidden
let showLater = false; // likewise, its first mark

// escaped makes text literal in a pattern.
function escaped(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// amountPattern finds "1.342,00" as 1342,00 / 1,342.00 / 1 342,00.
function amountPattern(text) {
  const digits = text.replace(/\D/g, "");
  if (digits.length < 3 || /^0+$/.test(digits)) {
    return null;
  }
  const cents = digits.slice(-2);
  const whole = digits.slice(0, -2).split("");
  const body = whole.map((d, i) => {
    const left = whole.length - i - 1;
    return left > 0 && left % 3 === 0 ? d + "[.,\\s']?" : d;
  }).join("");
  return new RegExp("(?<![\\d.,])" + body + "\\s?[.,]\\s?" + cents + "(?!\\d)", "g");
}

const MONTHS = ["Januar|January|Jan", "Februar|February|Feb", "März|Maerz|March|Mar", "April|Apr", "Mai|May", "Juni|June|Jun",
  "Juli|July|Jul", "August|Aug", "September|Sept|Sep", "Oktober|October|Okt|Oct", "November|Nov", "Dezember|December|Dez|Dec"];

// dayPattern finds a day in the ways receipts write it.
function dayPattern(iso) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!m) {
    return null;
  }
  const [, y, mo, da] = m;
  const day = da.replace(/^0/, "0?");
  const month = mo.replace(/^0/, "0?");
  const year = "(?:" + y + "|" + y.slice(2) + ")";
  const name = "(?:" + MONTHS[Number(mo) - 1] + ")";
  return new RegExp("(?<!\\d)(?:" + [
    day + "\\s?[./-]\\s?" + month + "\\s?[./-]\\s?" + year,
    y + "\\s?[-/.]\\s?" + mo + "\\s?[-/.]\\s?" + da,
    day + "\\.?\\s*" + name + "\\.?\\s*" + y,
    name + "\\.?\\s*" + day + ",?\\s*" + y,
  ].join("|") + ")(?!\\d)", "gi");
}

// patterns are what the frame's hash asks to mark.
function patterns() {
  const q = new URLSearchParams(location.hash.slice(1));
  const out = [];
  const add = (re) => {
    if (re) {
      out.push(re);
    }
  };
  add(amountPattern(q.get("a") || ""));
  add(dayPattern(q.get("d") || ""));
  const number = (q.get("n") || "").trim();
  if (number.length >= MIN_NUMBER) {
    out.push(new RegExp(escaped(number).replace(/\s+/g, "\\s*"), "gi"));
  }
  return out;
}

// markPage draws the marks on one page: each match in the text joined
// from its pieces, a box per piece it touches, in percent of the page.
function markPage(p, res) {
  p.box.querySelectorAll(".doc-mark").forEach((m) => m.remove());
  let text = "";
  const spans = p.items.map((item) => {
    const span = { item, from: text.length };
    text += item.str + " ";
    span.to = span.from + item.str.length;
    return span;
  });
  res.forEach((re) => {
    for (const m of text.matchAll(re)) {
      const end = m.index + m[0].length;
      spans.forEach((s) => {
        if (s.to <= m.index || s.from >= end || !s.item.str.length) {
          return;
        }
        const part = s.item;
        const n = part.str.length;
        const a = (Math.max(m.index, s.from) - s.from) / n;
        const b = (Math.min(end, s.to) - s.from) / n;
        const mark = document.createElement("i");
        mark.className = "doc-mark";
        mark.style.left = (part.left + a * part.width) * 100 + "%";
        mark.style.width = (b - a) * part.width * 100 + "%";
        mark.style.top = part.top * 100 + "%";
        mark.style.height = part.height * 100 + "%";
        p.box.appendChild(mark);
      });
    }
  });
}

// inView says whether the frame has a layout: hidden, Firefox answers no
// sizes at all and a page would draw as nothing.
function inView() {
  return pages.clientWidth > 0;
}

// showMark brings a mark into the frame's view, leaving the desk around
// the frame where the user is (scrollIntoView would move it too).
function showMark(mark) {
  if (!mark) {
    return;
  }
  if (!inView()) {
    showLater = true;
    return;
  }
  const at = mark.getBoundingClientRect().top - pages.getBoundingClientRect().top;
  pages.scrollTop += at - pages.clientHeight / 2;
}

function markAll() {
  const res = patterns();
  drawn.forEach((p) => markPage(p, res));
  showMark(pages.querySelector(".doc-mark"));
  root.setAttribute("data-marks", String(pages.querySelectorAll(".doc-mark").length));
}

// textOf turns a page's text into pieces with boxes in fractions of the page.
function textOf(content, viewport, util) {
  return content.items.filter((it) => it.str !== undefined).map((it) => {
    const t = util.transform(viewport.transform, it.transform);
    const height = Math.hypot(t[2], t[3]);
    return { str: it.str, left: t[4] / viewport.width, top: (t[5] - height) / viewport.height,
      width: it.width * viewport.scale / viewport.width, height: height / viewport.height };
  });
}

function failed() {
  pages.textContent = "";
  const p = document.createElement("p");
  p.className = "error small";
  p.textContent = root.getAttribute("data-failed");
  pages.appendChild(p);
  root.setAttribute("data-state", "failed");
}

// drawPage paints a page at the frame's width and zoom.
async function drawPage(p) {
  if (!inView()) {
    drawLater = true;
    return;
  }
  const room = pages.clientWidth - 2 * parseFloat(getComputedStyle(pages).paddingLeft);
  const base = p.page.getViewport({ scale: 1 });
  const css = room * zoom;
  const ratio = window.devicePixelRatio || 1;
  const viewport = p.page.getViewport({ scale: css / base.width * ratio });
  const canvas = document.createElement("canvas");
  canvas.width = viewport.width;
  canvas.height = viewport.height;
  await p.page.render({ canvas, viewport }).promise;
  p.box.querySelector("canvas")?.remove();
  p.box.prepend(canvas);
  p.box.style.width = css + "px";
}

async function drawAll() {
  for (const p of drawn) {
    await drawPage(p);
  }
}

async function showPdf(data) {
  const pdfjs = await import(root.getAttribute("data-pdfjs"));
  pdfjs.GlobalWorkerOptions.workerSrc = root.getAttribute("data-worker");
  const base = root.getAttribute("data-base");
  const doc = await pdfjs.getDocument({ data, isEvalSupported: false, enableScripting: false, cMapUrl: base + "cmaps/", cMapPacked: true,
    standardFontDataUrl: base + "standard_fonts/", wasmUrl: base + "wasm/" }).promise;
  for (let n = 1; n <= Math.min(doc.numPages, MAX_PAGES); n++) {
    const page = await doc.getPage(n);
    const box = document.createElement("div");
    box.className = "doc-page";
    pages.appendChild(box);
    const content = await page.getTextContent();
    const p = { page, box, items: textOf(content, page.getViewport({ scale: 1 }), pdfjs.Util) };
    drawn.push(p);
    await drawPage(p);
    if (n === 1) {
      root.setAttribute("data-state", "shown"); // the first page counts: the rest may take a while
    }
  }
  if (doc.numPages > MAX_PAGES) {
    const more = document.createElement("p");
    more.className = "small muted";
    more.textContent = root.getAttribute("data-more").replace("#shown", MAX_PAGES).replace("#total", doc.numPages);
    pages.appendChild(more);
  }
  markAll();
  root.setAttribute("data-state", "done");
}

function showImage(url) {
  const box = document.createElement("div");
  box.className = "doc-page";
  const img = document.createElement("img");
  img.alt = "";
  img.src = url;
  box.appendChild(img);
  pages.appendChild(box);
  drawn = [];
  root.setAttribute("data-state", "done");
  root.setAttribute("data-marks", "0");
}

// resize draws again for a new width; an image follows by itself.
let waiting = null;
function resized() {
  const width = pages.clientWidth;
  if (!width || width === lastWidth || !drawn.length) {
    lastWidth = width;
    return;
  }
  lastWidth = width;
  clearTimeout(waiting);
  waiting = setTimeout(drawAll, 200);
}

async function start() {
  const url = root.getAttribute("data-file");
  try {
    const res = await fetch(url);
    if (!res.ok) {
      failed();
      return;
    }
    const kind = res.headers.get("Content-Type") || "";
    if (kind.startsWith("image/")) {
      showImage(url);
      return;
    }
    await showPdf(await res.arrayBuffer());
  } catch {
    failed();
  }
}

root.querySelectorAll("[data-zoom]").forEach((btn) => {
  btn.addEventListener("click", () => {
    const by = Number(btn.getAttribute("data-zoom")) > 0 ? ZOOM_STEP : 1 / ZOOM_STEP;
    zoom = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, zoom * by));
    const img = pages.querySelector(".doc-page>img");
    if (img) {
      img.closest(".doc-page").style.width = zoom * 100 + "%";
      return;
    }
    drawAll();
  });
});
window.addEventListener("hashchange", markAll);

// The desk says when this frame shows again (andon.js benchDocShown):
// nothing here sees it, as Firefox reports no new size.
window.addEventListener("message", async (e) => {
  if (e.origin !== location.origin || e.data !== "doc-shown" || !inView()) {
    return;
  }
  if (drawLater) {
    drawLater = false;
    await drawAll();
  }
  if (showLater) {
    showLater = false;
    showMark(pages.querySelector(".doc-mark"));
  }
});
new ResizeObserver(resized).observe(pages);
start();
