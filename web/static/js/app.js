// Small progressive enhancements; every page works without JavaScript.
document.addEventListener('alpine:init', () => {
  // Step checklist that remembers progress per recipe in localStorage.
  Alpine.data('steps', (key, n) => ({
    done: (() => {
      try {
        const saved = JSON.parse(localStorage.getItem('steps:' + key));
        if (Array.isArray(saved) && saved.length === n) return saved;
      } catch (e) { /* ignore */ }
      return Array(n).fill(false);
    })(),
    init() { this.$watch('done', (v) => localStorage.setItem('steps:' + key, JSON.stringify(v))); },
    get count() { return this.done.filter(Boolean).length; },
    reset() { this.done = Array(n).fill(false); },
  }));
  // Browser geolocation → hidden form fields → server-side reverse geocoding.
  Alpine.data('locate', () => ({
    busy: false,
    failed: false,
    find(form) {
      if (!navigator.geolocation) { this.failed = true; return; }
      this.busy = true;
      navigator.geolocation.getCurrentPosition(
        (pos) => {
          form.lat.value = pos.coords.latitude.toFixed(4);
          form.lng.value = pos.coords.longitude.toFixed(4);
          form.submit();
        },
        () => { this.busy = false; this.failed = true; },
        { timeout: 10000, maximumAge: 600000 },
      );
    },
  }));

  // Serving-size scaler: multiplies the leading quantity of every ingredient
  // line (decimal or unicode fraction) by the chosen serving count. Lines
  // without a leading number are left alone; JS off = base servings.
  Alpine.data('servings', (base) => ({
    base,
    count: base,
    inc() { this.count = Math.min(this.count + 1, 99); this.apply(); },
    dec() { this.count = Math.max(this.count - 1, 1); this.apply(); },
    reset() { this.count = this.base; this.apply(); },
    apply() {
      const f = this.count / this.base;
      document.querySelectorAll('.ing-line[data-line]').forEach((el) => {
        el.textContent = el.dataset.qty ? scaleParts(el.dataset, f) : scaleLine(el.dataset.line, f);
      });
    },
  }));
});

// Unicode fractions → floats, and a compact formatter with the same glyphs
// the server uses (FormatQuantity).
const FRACTIONS = { '½': 0.5, '¼': 0.25, '¾': 0.75, '⅓': 1 / 3, '⅔': 2 / 3, '⅛': 0.125 };
const GLYPHS = [[0.125, '⅛'], [0.25, '¼'], [1 / 3, '⅓'], [0.5, '½'], [2 / 3, '⅔'], [0.75, '¾']];

// Server-composed line: base quantity, unit label per plural sample and the
// rest of the text arrive as data attributes, so "1 clove" becomes
// "2 cloves" (and ru "1 зубчик" → "5 зубчиков") with the page's CLDR rules.
const pluralRules = (() => {
  try { return new Intl.PluralRules(document.documentElement.lang || 'en'); } catch (e) { return null; }
})();

function unitFor(forms, n) {
  if (!forms) return '';
  let map;
  try { map = JSON.parse(forms); } catch (e) { return ''; }
  const samples = Object.keys(map);
  if (pluralRules) {
    // Match on the rounded value the reader sees (1.0000001 → "1" → one).
    const want = pluralRules.select(Math.round(n * 100) / 100);
    for (const s of samples) if (pluralRules.select(Number(s)) === want) return map[s];
  }
  return map[n === 1 ? '1' : '2'] || map[samples[0]];
}

function scaleParts(d, factor) {
  if (factor === 1) return d.line;
  const q = parseFloat(d.qty) * factor;
  const unit = unitFor(d.units, q);
  return [formatQty(q), unit, d.rest].filter(Boolean).join(' ');
}

// Free-text line (hand-written translations): scale its leading quantity
// and keep the rest. Returns [value, matched prefix] or null.
function leadingQty(text) {
  const num = (x) => parseFloat(x.replace(',', '.'));
  let m;
  if ((m = text.match(/^(\d+)\s+(\d+)\/(\d+)/)) && Number(m[3])) return [Number(m[1]) + m[2] / m[3], m[0]]; // 1 1/2
  if ((m = text.match(/^(\d+)\/(\d+)/)) && Number(m[2])) return [m[1] / m[2], m[0]]; // 1/2
  if ((m = text.match(/^(\d+(?:[.,]\d+)?)?\s*([½¼¾⅓⅔⅛])/u))) return [(m[1] ? num(m[1]) : 0) + FRACTIONS[m[2]], m[0]]; // 1 ½, ½
  if ((m = text.match(/^\d+(?:[.,]\d+)?/))) return [num(m[0]), m[0]]; // 2, 0.5, 1,5
  return null;
}

function scaleLine(text, factor) {
  if (factor === 1) return text;
  // "2-3 cloves" / "2–3": scale both ends of a range.
  const range = text.match(/^(\d+(?:[.,]\d+)?)\s*([-–])\s*(\d+(?:[.,]\d+)?)/);
  if (range) {
    const num = (x) => parseFloat(x.replace(',', '.'));
    return formatQty(num(range[1]) * factor) + range[2] + formatQty(num(range[3]) * factor) + text.slice(range[0].length);
  }
  if (/^\d+\s*\//.test(text) && !/^(\d+\s+)?\d+\/[1-9]/.test(text)) return text; // "1/0": not a quantity
  const lead = leadingQty(text);
  if (!lead) return text;
  const rest = text.slice(lead[1].length);
  // Keep exactly one space between the number and what follows.
  return formatQty(lead[0] * factor) + (rest && !rest.startsWith(' ') ? ' ' : '') + rest;
}

function formatQty(q) {
  const whole = Math.floor(q);
  const frac = q - whole;
  for (const [v, g] of GLYPHS) {
    if (Math.abs(frac - v) < 0.02) return whole === 0 ? g : `${whole} ${g}`;
  }
  if (frac < 0.02) return String(whole);
  if (Math.abs(frac - 1) < 0.02) return String(whole + 1);
  return (Math.round(q * 100) / 100).toString();
}
