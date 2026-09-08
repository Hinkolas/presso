<!-- Inline stroke icons, 24px grid. Names are the vocabulary the UI uses;
     add here rather than pasting SVG into pages. -->
<script lang="ts">
	export type IconName =
		| 'sites'
		| 'jobs'
		| 'system'
		| 'edge'
		| 'database'
		| 'cache'
		| 'grid'
		| 'list'
		| 'more'
		| 'clock'
		| 'external'
		| 'search'
		| 'plus'
		| 'refresh'
		| 'camera'
		| 'chevron-down'
		| 'chevron-right'
		| 'check'
		| 'close'
		| 'play'
		| 'stop'
		| 'trash'
		| 'key'
		| 'eye'
		| 'eye-off'
		| 'back'
		| 'warning'
		| 'pause'
		| 'broom'
		| 'terminal'
		| 'user'
		| 'logout'
		| 'settings'
		| 'monitor'
		| 'shield'
		| 'live'
		| 'menu';

	let {
		name,
		size = 16,
		class: klass = ''
	}: { name: IconName; size?: number; class?: string } = $props();

	const paths: Record<IconName, string> = {
		sites: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
		jobs: 'M4 6h16M4 12h10M4 18h7M18 15l2 2 4-4',
		system: 'M5 5h14v14H5zM9 9h6v6H9zM9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3',
		edge: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18',
		database:
			'M12 3c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
		cache: 'M13 2L4 14h7l-1 8 9-12h-7z',
		grid: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
		list: 'M4 6h16M4 12h16M4 18h16',
		more: 'M5 12h.01M12 12h.01M19 12h.01',
		clock: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 7v5l3 2',
		external: 'M14 4h6v6M20 4l-9 9M18 14v6H4V6h6',
		search: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM20 20l-4-4',
		plus: 'M12 5v14M5 12h14',
		refresh: 'M20 12a8 8 0 1 1-2.3-5.6M20 4v5h-5',
		camera: 'M4 8h3l2-3h6l2 3h3v12H4zM12 17a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7z',
		'chevron-down': 'M6 9l6 6 6-6',
		'chevron-right': 'M9 6l6 6-6 6',
		check: 'M5 12l5 5L20 7',
		close: 'M6 6l12 12M18 6L6 18',
		play: 'M7 5v14l12-7z',
		stop: 'M6 6h12v12H6z',
		trash: 'M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v6M14 11v6',
		key: 'M15 3a6 6 0 1 0 0 12 6 6 0 0 0 0-12zM9.5 14.5L3 21M6 18l2 2M3 21l2 2',
		eye: 'M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
		'eye-off':
			'M3 3l18 18M10.6 10.6A3 3 0 0 0 13.4 13.4M6.5 6.7C3.9 8.5 2 12 2 12s4 7 10 7c1.7 0 3.2-.4 4.5-1M9.9 5.2C10.6 5.1 11.3 5 12 5c6 0 10 7 10 7s-.9 1.6-2.5 3.2',
		back: 'M15 6l-6 6 6 6',
		warning: 'M12 3l10 18H2zM12 10v5M12 18v.5',
		pause: 'M8 5v14M16 5v14',
		broom: 'M15 3l6 6M9 9l6 6M4 20l5-11 6 6-11 5z',
		terminal: 'M4 5h16v14H4zM8 9l3 3-3 3M13 15h4',
		user: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 21a8 8 0 0 1 16 0',
		logout: 'M10 4H5v16h5M14 8l4 4-4 4M18 12H9',
		settings:
			'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z',
		monitor: 'M3 5h18v11H3zM8 20h8M12 16v4',
		shield: 'M12 3l8 3v6c0 4.5-3.4 8-8 9-4.6-1-8-4.5-8-9V6zM9 12l2 2 4-4',
		live: 'M12 12m-3 0a3 3 0 1 0 6 0 3 3 0 1 0-6 0M5 5a10 10 0 0 0 0 14M19 5a10 10 0 0 1 0 14',
		menu: 'M4 7h16M4 12h16M4 17h16'
	};
</script>

<svg
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="1.75"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
	class="shrink-0 {klass}"
>
	<path d={paths[name]} />
</svg>
