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
        el.textContent = scaleLine(el.dataset.line, f);
      });
    },
  }));
});

// Unicode fractions → floats, and a compact formatter with the same glyphs
// the server uses (FormatQuantity).
const FRACTIONS = { '½': 0.5, '¼': 0.25, '¾': 0.75, '⅓': 1 / 3, '⅔': 2 / 3, '⅛': 0.125 };
const GLYPHS = [[0.125, '⅛'], [0.25, '¼'], [1 / 3, '⅓'], [0.5, '½'], [2 / 3, '⅔'], [0.75, '¾']];

function scaleLine(text, factor) {
  if (factor === 1) return text;
  // "2", "2 ", "1 ½", "½", "0.5" … possibly followed by a unit word.
  const m = text.match(/^(?:\d+(?:[.,]\d+)?\s*)?(?:[½¼¾⅓⅔⅛])?\s*/u);
  const lead = m ? m[0] : '';
  if (lead.trim() === '') return text;
  let whole = 0, frac = 0, matched = false;
  const wm = lead.match(/^(\d+(?:[.,]\d+)?)/);
  const fm = lead.match(/[½¼¾⅓⅔⅛]/u);
  if (wm) { whole = parseFloat(wm[1].replace(',', '.')); matched = true; }
  if (fm) { frac = FRACTIONS[fm[0]]; matched = true; }
  if (!matched) return text;
  const scaled = (whole + frac) * factor;
  const rest = text.slice(lead.length);
  // lead's trailing space is consumed by the match; keep one before the unit.
  return formatQty(scaled) + (rest && !rest.startsWith(' ') ? ' ' : '') + rest;
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
