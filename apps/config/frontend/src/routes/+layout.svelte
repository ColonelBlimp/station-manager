<script lang="ts">

	let {children} = $props();
</script>

<header>
Header
</header>
<main class="relative">
	{@render children()}
</main>
