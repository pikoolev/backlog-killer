export interface Anchor {
  cssSegments: string[];
  xpath?: string;
  beforeText: string;
  insideText: string;
  afterText: string;
  tag: string;
  role?: string;
  aria?: string;
  fingerprint: string;
  rect: { x: number; y: number; w: number; h: number };
}

function unique(root: Document | ShadowRoot, selector: string) {
  try {
    return root.querySelectorAll(selector).length === 1;
  } catch {
    return false;
  }
}

function escapeSelector(value: string) {
  if (typeof CSS !== 'undefined' && CSS.escape) return CSS.escape(value);
  return value.replace(/[^a-zA-Z0-9_-]/g, (character) => `\\${character}`);
}

function segment(el: Element, root: Document | ShadowRoot) {
  const stable = el.getAttribute('data-backlog-id');
  if (stable) return `[data-backlog-id="${escapeSelector(stable)}"]`;
  if (el.id && unique(root, `#${escapeSelector(el.id)}`)) return `#${escapeSelector(el.id)}`;
  for (const name of ['data-testid', 'name', 'aria-label']) {
    const value = el.getAttribute(name);
    if (!value) continue;
    const selector = `${el.tagName.toLowerCase()}[${name}="${escapeSelector(value)}"]`;
    if (unique(root, selector)) return selector;
  }
  let selector = el.tagName.toLowerCase();
  const parent = el.parentElement;
  if (parent) {
    const siblings = [...parent.children].filter((candidate) => candidate.tagName === el.tagName);
    if (siblings.length > 1) selector += `:nth-of-type(${siblings.indexOf(el) + 1})`;
  }
  return selector;
}

function path(el: Element, root: Document | ShadowRoot) {
  const segments: string[] = [];
  let current: Element | null = el;
  while (current) {
    segments.unshift(segment(current, root));
    const candidate = segments.join(' > ');
    if (unique(root, candidate)) return candidate;
    current = current.parentElement;
  }
  return segments.join(' > ');
}

function normalizedText(el: Element) {
  return (el.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 300);
}

function fingerprint(value: string) {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(16);
}

export function captureAnchor(input: Element, x: number, y: number): Anchor {
  let element = input;
  const cssSegments: string[] = [];
  while (true) {
    const root = element.getRootNode() as Document | ShadowRoot;
    cssSegments.unshift(path(element, root));
    if (!(root instanceof ShadowRoot)) break;
    element = root.host;
  }
  const bounds = input.getBoundingClientRect();
  const insideText = normalizedText(input);
  const role = input.getAttribute('role');
  const aria = input.getAttribute('aria-label');
  const anchor: Anchor = {
    cssSegments,
    beforeText: '',
    insideText,
    afterText: '',
    tag: input.tagName.toLowerCase(),
    fingerprint: fingerprint([input.tagName, role, aria, insideText].join('|')),
    rect: {
      x: bounds.width ? (x - bounds.left) / bounds.width : 0,
      y: bounds.height ? (y - bounds.top) / bounds.height : 0,
      w: Math.min(1, 20 / Math.max(bounds.width, 20)),
      h: Math.min(1, 20 / Math.max(bounds.height, 20)),
    },
  };
  if (role) anchor.role = role;
  if (aria) anchor.aria = aria;
  return anchor;
}

export function anchorFingerprint(el: Element) {
  return fingerprint([el.tagName, el.getAttribute('role'), el.getAttribute('aria-label'), normalizedText(el)].join('|'));
}
