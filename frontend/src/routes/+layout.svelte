<script lang="ts">
	import './layout.css';
	import MainNav from "$lib/ui/MainNav.svelte";
	import {SvelteToast} from "@zerodevx/svelte-toast";
	import {setFocusContext} from "@station-manager/shared-utils/svelte";

	let { children } = $props();

	// Initialize focus context for cross-component focus management
	setFocusContext();
</script>

<header>
	<SvelteToast/>
	<MainNav/>
</header>
<main class="relative">
	{@render children()}
</main>
