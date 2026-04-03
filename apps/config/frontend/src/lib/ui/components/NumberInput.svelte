<script lang="ts">
    interface Props {
        value: string;
        onChange: (v: string) => void;
    }

    let { value, onChange }: Props = $props();
</script>

<input
    type="text"
    {value}
    oninput={(e) => onChange(e.currentTarget.value)}
    class="w-36 rounded border-gray-300 shadow-sm focus:border-indigo-500 focus:ring focus:ring-indigo-500 focus:ring-opacity-50"
/>
