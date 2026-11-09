// Mayoi landing — theme toggle, install tabs, copy buttons.
(() => {
  const root = document.documentElement;

  // Theme toggle: flips between light and dark, remembered per browser.
  const toggle = document.querySelector('[data-theme-toggle]');
  const isDark = () => root.dataset.theme
    ? root.dataset.theme === 'dark'
    : matchMedia('(prefers-color-scheme: dark)').matches;
  const label = () => toggle.setAttribute('aria-label', isDark() ? 'Switch to light mode' : 'Switch to dark mode');
  label();
  toggle.addEventListener('click', () => {
    root.dataset.theme = isDark() ? 'light' : 'dark';
    try { localStorage.setItem('mayoi-theme', root.dataset.theme); } catch (e) {}
    label();
  });

  // Install tabs: click or arrow keys, as the ARIA tabs pattern says.
  const tabs = [...document.querySelectorAll('[role="tab"]')];
  const select = (tab, focus) => {
    for (const t of tabs) {
      const on = t === tab;
      t.setAttribute('aria-selected', on);
      t.tabIndex = on ? 0 : -1;
      document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
    }
    if (focus) tab.focus();
  };
  for (const tab of tabs) {
    tab.addEventListener('click', () => select(tab));
    tab.addEventListener('keydown', (e) => {
      const i = tabs.indexOf(tab);
      const next = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: tabs.length - 1 }[e.key];
      if (next === undefined) return;
      e.preventDefault();
      select(tabs[(next + tabs.length) % tabs.length], true);
    });
  }
  // Start on the visitor's system when we can tell.
  const ua = navigator.userAgent;
  if (/Windows/.test(ua)) select(document.getElementById('tab-win'));
  else if (/Linux/.test(ua) && !/Android/.test(ua)) select(document.getElementById('tab-linux'));

  // Theme preview: the chips and Dark/Light recolor the mock window with Mayoi's own palettes.
  const mock = document.querySelector('[data-mock]');
  const caption = document.querySelector('[data-mock-caption]');
  const chips = [...document.querySelectorAll('.chip')];
  const segs = [...document.querySelectorAll('[data-appearance]')];
  const note = document.querySelector('[data-appearance-note]');
  let theme = chips[0];
  let appearance = 'dark';
  const paint = () => {
    const both = theme.dataset.both === 'true';
    const mode = both ? appearance : 'dark';
    const id = theme.dataset.themeId;
    mock.dataset.pal = both ? `${id}-${mode}` : id;
    const name = theme.textContent.trim();
    mock.setAttribute('aria-label', `Mayoi's editor in the ${name} theme, ${mode}, showing theme.go, the Explorer and a terminal`);
    caption.textContent = both ? `${name} · ${mode}` : `${name} · always dark`;
    for (const c of chips) c.setAttribute('aria-pressed', c === theme);
    for (const s of segs) {
      s.disabled = !both;
      s.setAttribute('aria-pressed', s.dataset.appearance === mode);
    }
    note.hidden = both;
  };
  for (const c of chips) c.addEventListener('click', () => { theme = c; paint(); });
  for (const s of segs) s.addEventListener('click', () => { appearance = s.dataset.appearance; paint(); });

  // Copy buttons copy the command block beside them.
  for (const btn of document.querySelectorAll('[data-copy]')) {
    btn.addEventListener('click', async () => {
      const text = btn.parentElement.querySelector('code').textContent;
      try { await navigator.clipboard.writeText(text); } catch (e) { return; }
      btn.textContent = 'Copied';
      btn.dataset.done = '';
      setTimeout(() => { btn.textContent = 'Copy'; delete btn.dataset.done; }, 1600);
    });
  }
})();
