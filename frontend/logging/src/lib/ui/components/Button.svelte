<script lang="ts">
    interface Props {
        id: string;
        label: string;
    }

    let { id, label }: Props = $props();
</script>

<button {id} type="button" class="btn btn-secondary w-14">{label}</button>
