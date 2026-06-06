<script lang="ts">
    interface Props {
        id: string;
        label: string;
    }

    let { id, label }: Props = $props();
</script>

<button id={id} type="button" class="btn btn-secondary w-full">{label}</button>
