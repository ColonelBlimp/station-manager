<script lang="ts">
    interface Props {
        label: string;
    }

    let { label }: Props = $props();
</script>

<button class="btn btn-primary">{label}</button>
