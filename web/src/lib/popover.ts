// Places a floating panel next to its trigger in viewport coordinates, so it
// escapes scroll containers (the content pane, tables) that would clip an
// absolutely positioned child. Flips above when there is no room below.

export type Placement = { top: number; left: number; minWidth: number; up: boolean };

export function placePanel(
	anchor: HTMLElement,
	panel: HTMLElement,
	align: 'left' | 'right' = 'left',
	gap = 8
): Placement {
	const a = anchor.getBoundingClientRect();
	const p = panel.getBoundingClientRect();
	const vw = window.innerWidth;
	const vh = window.innerHeight;
	const up = a.bottom + gap + p.height > vh - 8 && a.top - gap - p.height > 8;
	const top = up ? a.top - gap - p.height : a.bottom + gap;
	let left = align === 'right' ? a.right - p.width : a.left;
	left = Math.max(8, Math.min(left, vw - p.width - 8));
	return { top, left, minWidth: a.width, up };
}
