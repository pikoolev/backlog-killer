{
type Screen = {url: string; title: string; tabs: {selector: string; label: string}[]};
type Point = {x: number; y: number; viewportWidth: number; viewportHeight: number; scrollX: number; scrollY: number};
type AnchorSelection = {
  cssSegments: string[];
  tag: string;
  insideText: string;
  rect: {x: number; y: number; w: number; h: number};
  screen?: Screen;
  point?: Point;
};
type Ticket = {id: string; text: string; status: string; priority: number; page: {urlPath: string}; anchor?: Partial<AnchorSelection>};

// Dynamic injection and extension updates may execute the content script more than once in one document.
const canvasRuntime = globalThis as typeof globalThis & {__backlogKillerContentCleanup?: () => void};
canvasRuntime.__backlogKillerContentCleanup?.();
document.getElementById('backlog-killer-canvas')?.remove();

const host = document.createElement('div');
host.id = 'backlog-killer-canvas';
const shadow = host.attachShadow({mode: 'closed'});
const style = document.createElement('style');
style.textContent = `
  :host { all: initial; position: fixed; inset: 0; z-index: 2147483646; pointer-events: none; }
  .canvas { position: fixed; inset: 0; pointer-events: none; font: 12px Inter, Arial, sans-serif; }
  .picker { position: fixed; inset: 0; cursor: crosshair; pointer-events: auto; background: transparent; touch-action: none; }
  .highlight { position: fixed; display: none; box-sizing: border-box; border: 3px solid #7c3aed; border-radius: 3px; pointer-events: none; }
  .hint { position: fixed; left: 16px; bottom: 16px; padding: 8px 12px; color: white; background: #312e81; border-radius: 6px; pointer-events: none; }
  .sticky { position: fixed; box-sizing: border-box; width: 174px; min-height: 88px; padding: 12px 13px 10px; background: #fde047; color: #422006; border: 1px solid #eab308; border-radius: 3px 3px 12px 3px; box-shadow: 0 8px 22px #42200638; pointer-events: auto; cursor: pointer; transform: rotate(-1.2deg); overflow: hidden; text-align: left; }
  .sticky:nth-child(even) { transform: rotate(1.1deg); background: #fef08a; }
  .sticky:before { content: ''; position: absolute; right: 0; bottom: 0; border-style: solid; border-width: 0 0 18px 18px; border-color: transparent transparent #facc15 transparent; }
  .sticky b { display: block; font-size: 12px; line-height: 1.35; max-height: 48px; overflow: hidden; }
  .sticky span { display: block; margin-top: 8px; font-size: 10px; }
  .draft { pointer-events: none; background: #fef08a; opacity: .9; }
  .pulse { animation: pulse .4s ease; }
  @keyframes pulse { 50% { transform: scale(1.09) rotate(-1deg); } }
`;
const canvas = document.createElement('div');
canvas.className = 'canvas';
shadow.append(style, canvas);

let selecting = false;
let picker: HTMLDivElement | null = null;
let draft: AnchorSelection | null = null;
let tickets: Ticket[] = [];
let screenKey = '';
let positionFrame = 0;
let contextTimer: number | undefined;
let previousUrl = location.href;

const ensureHost = () => {
  if (!host.isConnected) (document.body || document.documentElement).append(host);
};
const selectorFor = (element: Element): string => {
  const parts: string[] = [];
  let current: Element | null = element;
  while (current && current !== document.documentElement) {
    if (current.id) {
      parts.unshift(`#${CSS.escape(current.id)}`);
      break;
    }
    let part = current.tagName.toLowerCase();
    const parent: Element | null = current.parentElement;
    if (parent) {
      let count = 0;
      let index = 0;
      for (const sibling of parent.children) {
        if (sibling.tagName === current.tagName) {
          count++;
          if (sibling === current) index = count;
        }
      }
      if (count > 1) part += `:nth-of-type(${index})`;
    }
    parts.unshift(part);
    current = parent;
  }
  return parts.join(' > ');
};
const activeTabSelector = '[role="tab"][aria-selected="true"], [role="tab"][data-state="active"], [role="tab"][data-active="true"], [role="tab"][aria-current="page"], [role="tab"][aria-current="true"], nav a[aria-current="page"], nav a[aria-current="true"], [role="navigation"] a[aria-current="page"], [role="navigation"] a[aria-current="true"]';
const currentScreen = (): Screen => {
  const selected = new Set<Element>(document.querySelectorAll(activeTabSelector));
  // Many SPAs expose the current internal section only through the exact href of its nav link.
  for (const link of document.querySelectorAll<HTMLAnchorElement>('nav a[href], aside a[href], [role="navigation"] a[href], main a[href]')) {
    if (link.href === location.href) selected.add(link);
  }
  return {
    url: location.href,
    title: document.title,
    tabs: [...selected]
      .filter(tab => !host.contains(tab))
      .map(tab => ({selector: selectorFor(tab), label: ((tab.getAttribute('aria-label') || tab.textContent) ?? '').trim().replace(/\s+/g, ' ').slice(0, 120)}))
      .sort((a, b) => a.selector.localeCompare(b.selector)),
  };
};
const sameScreen = (saved: Screen, current: Screen): boolean =>
  saved.url === current.url && saved.tabs.length === current.tabs.length &&
  saved.tabs.every((tab, index) => tab.selector === current.tabs[index]?.selector && tab.label === current.tabs[index]?.label);
const visibleOnScreen = (anchor: Partial<AnchorSelection> | undefined, page: {urlPath: string}, screen: Screen): boolean =>
  anchor?.screen ? sameScreen(anchor.screen, screen) : page.urlPath === location.pathname;
const resolvedElement = (anchor: Partial<AnchorSelection> | undefined): Element | null => {
  for (const selector of anchor?.cssSegments ?? []) {
    try {
      const element = document.querySelector(selector);
      if (element && !host.contains(element)) return element;
    } catch { /* Old or malformed selectors use the saved point instead. */ }
  }
  return null;
};
const position = (note: HTMLElement, anchor: Partial<AnchorSelection> | undefined): void => {
  const element = resolvedElement(anchor);
  const rect = element?.getBoundingClientRect();
  const relative = anchor?.rect;
  const point = anchor?.point;
  const x = rect && relative ? rect.left + rect.width * relative.x : point ? point.x - window.scrollX : rect ? rect.right : innerWidth - 190;
  const y = rect && relative ? rect.top + rect.height * relative.y : point ? point.y - window.scrollY : rect ? rect.top : 24;
  // An off-screen attachment stays at its actual location instead of sticking to the viewport edge.
  note.hidden = !!(rect || point) && (!Number.isFinite(x) || !Number.isFinite(y) || x < -174 || y < -88 || x > innerWidth || y > innerHeight);
  note.style.left = `${x - 18}px`;
  note.style.top = `${y - 16}px`;
};
const schedulePosition = () => {
  if (positionFrame) return;
  positionFrame = requestAnimationFrame(() => {
    positionFrame = 0;
    for (const note of canvas.querySelectorAll<HTMLElement>('.sticky')) {
      if (note.dataset.draft) position(note, draft ?? undefined);
      else position(note, tickets.find(ticket => ticket.id === note.dataset.ticketId)?.anchor);
    }
  });
};
const render = () => {
  ensureHost();
  const screen = currentScreen();
  canvas.replaceChildren();
  for (const ticket of tickets) {
    if (ticket.status === 'deleted' || !visibleOnScreen(ticket.anchor, ticket.page, screen)) continue;
    const note = document.createElement('button');
    note.type = 'button';
    note.className = 'sticky';
    note.dataset.ticketId = ticket.id;
    const title = document.createElement('b');
    title.textContent = ticket.text;
    const meta = document.createElement('span');
    meta.textContent = `${'★'.repeat(Math.max(0, Math.min(5, ticket.priority)))} · ${ticket.status}`;
    note.append(title, meta);
    note.addEventListener('click', () => {
      note.classList.remove('pulse');
      void note.offsetWidth;
      note.classList.add('pulse');
      resolvedElement(ticket.anchor)?.scrollIntoView({behavior: 'smooth', block: 'center'});
      void chrome.runtime.sendMessage({type: 'ticket-click', ticketId: ticket.id}).catch(() => {});
    });
    position(note, ticket.anchor);
    canvas.append(note);
  }
  if (draft && draft.screen && sameScreen(draft.screen, screen)) {
    const note = document.createElement('div');
    note.className = 'sticky draft';
    note.dataset.draft = 'true';
    const title = document.createElement('b');
    title.textContent = 'Новая заметка';
    note.append(title);
    position(note, draft);
    canvas.append(note);
  }
  if (picker) canvas.append(picker);
};
const announceScreen = () => {
  const screen = currentScreen();
  const key = JSON.stringify([screen.url, screen.title, screen.tabs]);
  if (key === screenKey) return;
  if (draft?.screen && !sameScreen(draft.screen, screen)) draft = null;
  if (selecting && previousUrl !== screen.url) stopSelection();
  screenKey = key;
  previousUrl = screen.url;
  render();
  void chrome.runtime.sendMessage({type: 'page-context', screen, page: {urlPath: location.pathname, title: screen.title}}).catch(() => {});
};
const scheduleContext = () => {
  if (contextTimer !== undefined) return;
  contextTimer = setTimeout(() => {
    contextTimer = undefined;
    announceScreen();
  }, 60);
};
const underlyingElement = (x: number, y: number): Element | null => {
  // Hide the entire shadow host while hit-testing, including its clickable saved notes.
  host.style.display = 'none';
  const target = document.elementFromPoint(x, y);
  host.style.display = '';
  return target;
};
const updateHighlight = (event: MouseEvent) => {
  if (!picker) return;
  const highlight = picker.querySelector<HTMLElement>('.highlight');
  const target = underlyingElement(event.clientX, event.clientY);
  if (!highlight) return;
  if (!target) {
    highlight.style.display = 'none';
    return;
  }
  const rect = target.getBoundingClientRect();
  highlight.style.display = 'block';
  highlight.style.left = `${rect.left}px`;
  highlight.style.top = `${rect.top}px`;
  highlight.style.width = `${rect.width}px`;
  highlight.style.height = `${rect.height}px`;
};
const stopSelection = (clearDraft = false) => {
  selecting = false;
  picker?.remove();
  picker = null;
  if (clearDraft) {
    draft = null;
    render();
  }
};
const escapeSelection = (event: KeyboardEvent) => {
  if (event.key !== 'Escape' || (!selecting && !draft)) return;
  event.preventDefault();
  event.stopImmediatePropagation();
  stopSelection(true);
  void chrome.runtime.sendMessage({type: 'selection-cancelled'}).catch(() => {});
};
const pick = (event: MouseEvent) => {
  event.preventDefault();
  event.stopPropagation();
  if (!selecting || event.button !== 0) return;
  const target = underlyingElement(event.clientX, event.clientY);
  if (!target) return;
  const rect = target.getBoundingClientRect();
  const screen = currentScreen();
  const selection: AnchorSelection = {
    cssSegments: [selectorFor(target)],
    tag: target.tagName.toLowerCase(),
    insideText: target.closest('form,[contenteditable],input,textarea,select') || target.querySelector('form,[contenteditable],input,textarea,select') ? '' : (target.textContent ?? '').trim().slice(0, 200),
    rect: {
      x: Math.max(0, Math.min(1, (event.clientX - rect.left) / Math.max(1, rect.width))),
      y: Math.max(0, Math.min(1, (event.clientY - rect.top) / Math.max(1, rect.height))),
      w: 1, h: 1,
    },
    screen,
    point: {x: event.clientX + window.scrollX, y: event.clientY + window.scrollY, viewportWidth: innerWidth, viewportHeight: innerHeight, scrollX: window.scrollX, scrollY: window.scrollY},
  };
  draft = selection;
  stopSelection();
  render();
  void chrome.runtime.sendMessage({type: 'selection', selection, page: {urlPath: location.pathname, title: document.title}}).catch(() => {});
};
const blockPickerEvent = (event: Event) => {
  event.preventDefault();
  event.stopPropagation();
};
const startSelection = () => {
  stopSelection(true);
  ensureHost();
  selecting = true;
  picker = document.createElement('div');
  picker.className = 'picker';
  const highlight = document.createElement('div');
  highlight.className = 'highlight';
  const hint = document.createElement('div');
  hint.className = 'hint';
  hint.textContent = 'Левый клик — отметить место · Esc — отмена';
  picker.append(highlight, hint);
  picker.addEventListener('mousemove', updateHighlight);
  picker.addEventListener('click', pick);
  for (const type of ['pointerdown', 'pointerup', 'mousedown', 'mouseup', 'dblclick', 'contextmenu', 'auxclick', 'touchstart', 'touchend']) {
    picker.addEventListener(type, blockPickerEvent);
  }
  canvas.append(picker);
};
const onMessage = (message: {type?: string; tickets?: Ticket[]}) => {
  if (message.type === 'ping') return Promise.resolve({ok: true, url: location.href, screen: currentScreen()});
  if (message.type === 'start-selection' || message.type === 'start-selection-v2') {
    startSelection();
    return Promise.resolve({ok: true});
  }
  if (message.type === 'stop-selection' || message.type === 'stop-selection-v2' || message.type === 'cancel-selection') {
    stopSelection(true);
    return Promise.resolve({ok: true});
  }
  if (message.type === 'render-tickets' || message.type === 'render-tickets-v2') {
    tickets = message.tickets ?? [];
    draft = null;
    render();
    return Promise.resolve({ok: true});
  }
};
const relevantMutation = (record: MutationRecord): boolean => {
  if (host === record.target || host.contains(record.target)) return false;
  if (record.type === 'attributes') return record.target instanceof Element &&
    (record.target.matches('[role="tab"], nav a, aside a, main a, [role="navigation"] a') || !!record.target.closest('[role="tab"]'));
  if (record.target instanceof Element && record.target.closest('[role="tab"], nav, aside, [role="navigation"], title')) return true;
  for (const node of [...record.addedNodes, ...record.removedNodes]) {
    if (node === host) continue;
    if (node instanceof Element && (node.matches('[role="tab"], nav, aside, [role="navigation"], title') || !!node.querySelector('[role="tab"], nav, aside, [role="navigation"], title') || (node.matches('a[href]') && !!node.closest('main')))) return true;
  }
  return false;
};
const observer = new MutationObserver(records => {
  if (!host.isConnected) ensureHost();
  if (location.href !== previousUrl || records.some(relevantMutation)) scheduleContext();
  if (tickets.length || draft) schedulePosition();
});
observer.observe(document.documentElement, {subtree: true, childList: true, attributes: true, attributeFilter: ['aria-selected', 'aria-current', 'aria-label', 'data-state', 'data-active', 'role', 'id']});
const onNavigation = () => scheduleContext();
const onViewportChange = () => schedulePosition();
const messageListener = (message: {type?: string; tickets?: Ticket[]}, _sender: chrome.runtime.MessageSender, sendResponse: (response?: unknown) => void): boolean => {
  const response = onMessage(message);
  if (response === undefined) return false;
  void Promise.resolve(response).then(sendResponse);
  return true;
};
chrome.runtime.onMessage.addListener(messageListener);
document.addEventListener('keydown', escapeSelection, true);
window.addEventListener('popstate', onNavigation);
window.addEventListener('hashchange', onNavigation);
window.addEventListener('scroll', onViewportChange, true);
window.addEventListener('resize', onViewportChange);
// history.pushState runs in the page's JS realm; a cheap URL comparison covers silent SPA navigation.
const urlCheck = window.setInterval(() => {
  if (location.href !== previousUrl) scheduleContext();
}, 800);
canvasRuntime.__backlogKillerContentCleanup = () => {
  stopSelection();
  observer.disconnect();
  chrome.runtime.onMessage.removeListener(messageListener);
  document.removeEventListener('keydown', escapeSelection, true);
  window.removeEventListener('popstate', onNavigation);
  window.removeEventListener('hashchange', onNavigation);
  window.removeEventListener('scroll', onViewportChange, true);
  window.removeEventListener('resize', onViewportChange);
  window.clearInterval(urlCheck);
  if (contextTimer !== undefined) clearTimeout(contextTimer);
  if (positionFrame) cancelAnimationFrame(positionFrame);
  host.remove();
};
ensureHost();
announceScreen();
}
