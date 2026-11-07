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
});
