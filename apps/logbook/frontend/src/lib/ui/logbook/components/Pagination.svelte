<script lang="ts">
    import {configState} from "$lib/states/config-state.svelte";

    interface Props {
        callback: (page: number, pageSize: number) => void;
        pageNum: number;
        totalItems: number;
    }

    let {callback = $bindable(), pageNum, totalItems}: Props = $props();
    let startIndex: number = $state(0);
    let pageCount: number = $state(1); // Total number of pages
    let endIndex: number = $state(1); // Number of items on the last page
    let pageSize: number = $state(configState.pageSize);

    const update = (pgNum: number, total: number): {startInx: number, endInx: number} => {
        if (pgNum < 1) {
            return {startInx: 0, endInx: 0};
        }
        const start = (pgNum * pageSize) - pageSize + 1;
        const end = pgNum * pageSize;
        if (end > total) {
            return {startInx: start, endInx: total};
        }
        return {startInx: start, endInx: end};
    }

    const onClickNext = () => {
        if (pageNum >= pageCount) {
            return;
        }
        pageNum++;
        const up = update(pageNum, totalItems);
        startIndex = up.startInx;
        endIndex = up.endInx;
        callback(pageNum, pageSize);
    }

    const onClickPrevious = () => {
        if (pageNum <= 1) {
            return;
        }
        pageNum--;
        const up = update(pageNum, totalItems);
        startIndex = up.startInx;
        endIndex = up.endInx;
        callback(pageNum, pageSize);
    }

    const onChangePageSize = (event: Event) => {
        const target = event.currentTarget as HTMLSelectElement;
        const value = target.value;
        pageSize = value === 'all' ? totalItems : parseInt(value, 10);
        pageNum = 1;
        callback(pageNum, pageSize);
    }

    $effect(() => {
        const up = update(pageNum, totalItems);
        startIndex = up.startInx;
        endIndex = up.endInx;
        pageCount = Math.ceil(totalItems / pageSize);
    });
</script>

<div class="sticky flex flex-row h-11 bg-white items-center justify-between px-2">
    <div class="flex flex-row space-x-2 items-center text-sm text-gray-700">
        <label for="page_size" class="ml-2">Page Size</label>
        <select
                onchange={onChangePageSize}
                id="page_size"
                class="w-16 focus:outline-2 focus:outline-indigo-600 rounded-md">
            <option value="{configState.pageSize}">{configState.pageSize}</option>
            <option value="{Math.floor(configState.pageSize*2)}">{Math.floor(configState.pageSize*2)}</option>
            <option value="{Math.floor(configState.pageSize*4)}">{Math.floor(configState.pageSize*4)}</option>
            <option value="{Math.floor(configState.pageSize*8)}">{Math.floor(configState.pageSize*8)}</option>
            <option value="all">All</option>
        </select>
    </div>
    <div class="text-sm text-gray-700">
        <p>
            Showing
            <span class="font-medium">{startIndex}</span>
            to
            <span class="font-medium">{endIndex}</span>
            of
            <span class="font-medium">{totalItems}</span>
            entries
        </p>
    </div>
    <div class="flex flex-row space-x-2">
        <div>
            <button onclick={onClickPrevious} class="text-sm cursor-pointer w-20 border border-gray-300 rounded-md py-1 hover:border-gray-400 hover:bg-gray-100">Previous</button>
        </div>
        <div>
            <button onclick={onClickNext} class="text-sm cursor-pointer w-20 border border-gray-300 rounded-md py-1 hover:border-gray-400 hover:bg-gray-100">Next</button>
        </div>
    </div>
</div>