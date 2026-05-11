<script lang="ts">
    import { putConfig } from '../../api/config';
    import { catState } from '../../states/cat.svelte';
    import { configState } from '../../states/config.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { qsoDefaults } from '../../states/qsoDefaults.svelte';
    import { toasts } from '../../states/toasts.svelte';
    import { isValidCallsign } from '../../validators/callsign';
    import { isValidMaidenhead } from '../../validators/maidenhead';
    import { passthrough } from '../../validators/passthrough';
    import { isValidCqZone, isValidItuZone, isValidDxcc } from '../../validators/zone';
    import ValidatedInput from '../components/ValidatedInput.svelte';

    /*
        Update flow. PUT /v1/config with the current logging-station
        snapshot; on success re-hydrate from the response so the
        daemon's normalisations land in the UI:
          - station_callsign upper-cased
          - my_gridsquare canonicalised (upper field, lower subsquare)
          - my_lat / my_lon derived from my_gridsquare
        The local input bindings are mutated in-place by configState's
        reactivity, so the operator sees the canonical form appear
        without a manual refresh. my_lat / my_lon are NOT sent —
        they're daemon-derived and the wire write of stale values
        would just be wasted bytes.
    */
    let saving = $state(false);

    async function onUpdate(): Promise<void> {
        if (saving) return;
        saving = true;
        try {
            const ls = configState.loggingStation;
            const outcome = await putConfig({
                logging_station: {
                    station_callsign: ls.stationCallsign,
                    operator: ls.operator,
                    owner_callsign: ls.ownerCallsign,
                    my_name: ls.myName,
                    my_gridsquare: ls.myGridsquare,
                    my_street: ls.myStreet,
                    my_city: ls.myCity,
                    my_postal_code: ls.myPostalCode,
                    my_country: ls.myCountry,
                    my_altitude: ls.myAltitude,
                    my_cq_zone: ls.myCqZone,
                    my_itu_zone: ls.myItuZone,
                    my_dxcc: ls.myDxcc,
                    my_rig: ls.myRig,
                    my_antenna: ls.myAntenna,
                    my_morse_key_type: ls.myMorseKeyType,
                    my_morse_key_info: ls.myMorseKeyInfo,
                },
                station: {
                    amp_enabled: configState.station.ampEnabled,
                    amp_multiplier: configState.station.ampMultiplier,
                    default_power: configState.station.defaultPower,
                },
            });
            switch (outcome.kind) {
                case 'ok':
                    configState.applyResponse(outcome.config);
                    if (qsoDefaults.notifyConfigSaved) {
                        toasts.info('Station updated.');
                    }
                    break;
                case 'validation':
                    console.warn(`[my-station update] ${outcome.code}: ${outcome.message}`);
                    toasts.error(outcome.message);
                    break;
                case 'server':
                    console.error(`[my-station update] ${outcome.code}: ${outcome.message}`);
                    toasts.error('Could not save station details. Try again.');
                    break;
                case 'network':
                    console.error(`[my-station update] daemon unreachable: ${outcome.message}`);
                    toasts.error('Cannot reach the daemon — check it is running.');
                    break;
            }
        } finally {
            saving = false;
        }
    }

    /*
        Sub-tabs inside My Station. Identity is the first-load tab
        (most-edited at first run); the rest are edit-once-ish. Same
        ARIA pattern as InfoPanel's outer tabs (tablist / tab /
        tabpanel) so screen readers see the nesting correctly.
    */
    type SectionId = 'identity' | 'location' | 'equipment' | 'modes' | 'cw' | 'qso';

    interface Section {
        id: SectionId;
        title: string;
    }

    const sections: Section[] = [
        { id: 'identity', title: 'Identity' },
        { id: 'location', title: 'Location' },
        { id: 'equipment', title: 'Equipment' },
        { id: 'modes', title: 'Mode Mappings' },
        { id: 'cw', title: 'CW' },
        { id: 'qso', title: 'QSO' },
    ];

    /*
        Persist the active sub-tab to sessionStorage so a page refresh
        keeps the operator on whichever section they were editing.
        sessionStorage tier per the persistence-layers doc — survives
        reload, resets on tab close. Same try/catch shape as
        SessionTimer.svelte for the private-browsing / disabled-storage
        edge cases. Fall back to 'identity' (the default first-load
        tab) when the stored value is missing or unrecognised.
    */
    const ACTIVE_SECTION_KEY = 'sm.myStation.activeSection';
    const VALID_SECTIONS: readonly SectionId[] = [
        'identity',
        'location',
        'equipment',
        'modes',
        'cw',
        'qso',
    ];

    function loadActiveSection(): SectionId {
        try {
            const raw = sessionStorage.getItem(ACTIVE_SECTION_KEY);
            if (raw !== null && (VALID_SECTIONS as readonly string[]).includes(raw)) {
                return raw as SectionId;
            }
        } catch {
            // sessionStorage unavailable — fall through to default.
        }
        return 'identity';
    }

    let activeSection: SectionId = $state(loadActiveSection());

    /*
        Local editing state for the Mode Mappings sub-tab. Snapped
        from configState.bridge.modeMappings each time the operator
        navigates INTO the tab — that way an external config refresh
        doesn't stomp in-progress edits while the operator is mid-
        change. On successful save the snap is taken from the
        daemon's response (which has the merged view post-write).
    */
    interface EditablePair {
        mode: string;
        submode: string;
    }
    let editingModes: Record<string, EditablePair> = $state({});
    let savingModes: boolean = $state(false);

    function snapModeMappings(): void {
        const fresh: Record<string, EditablePair> = {};
        for (const rigStr of configState.bridge.rigModes) {
            const m = configState.bridge.modeMappings[rigStr];
            fresh[rigStr] = { mode: m?.mode ?? '', submode: m?.submode ?? '' };
        }
        editingModes = fresh;
    }

    // $effect.pre runs BEFORE the DOM patches that follow a state
    // change. That ordering matters here: clicking the Mode Mappings
    // tab sets activeSection='modes', the template re-renders the
    // `{:else if activeSection === 'modes'}` branch, and the
    // `bind:value={editingModes[rigStr].mode}` inputs need a non-
    // undefined entry for every rigStr they iterate over. A plain
    // $effect would populate editingModes AFTER the failed render —
    // $effect.pre lands the population before the template binds.
    $effect.pre(() => {
        if (activeSection === 'modes') {
            snapModeMappings();
        }
    });

    async function saveModeMappings(): Promise<void> {
        if (savingModes) return;
        savingModes = true;
        try {
            const payload: Record<string, { mode: string; submode?: string }> = {};
            for (const [rigStr, pair] of Object.entries(editingModes)) {
                const mode = pair.mode.trim();
                if (mode === '') continue; // skip blanks; operator can clear an override by emptying mode
                const submode = pair.submode.trim();
                payload[rigStr] = submode ? { mode, submode } : { mode };
            }
            const outcome = await putConfig({
                bridge: {
                    enabled: configState.station.enabled,
                    driver: configState.bridge.driver,
                    mode_mappings: payload,
                },
            });
            switch (outcome.kind) {
                case 'ok':
                    configState.applyResponse(outcome.config);
                    snapModeMappings();
                    if (qsoDefaults.notifyConfigSaved) {
                        toasts.info('Mode mappings updated.');
                    }
                    break;
                case 'validation':
                    console.warn(`[mode-mappings save] ${outcome.code}: ${outcome.message}`);
                    toasts.error(outcome.message);
                    break;
                case 'server':
                    console.error(`[mode-mappings save] ${outcome.code}: ${outcome.message}`);
                    toasts.error('Could not save mode mappings. Try again.');
                    break;
                case 'network':
                    console.error(
                        `[mode-mappings save] daemon unreachable: ${outcome.message}`,
                    );
                    toasts.error('Cannot reach the daemon — check it is running.');
                    break;
            }
        } finally {
            savingModes = false;
        }
    }

    $effect(() => {
        try {
            sessionStorage.setItem(ACTIVE_SECTION_KEY, activeSection);
        } catch {
            // Storage write failed — in-memory state is still correct;
            // we lose refresh-survival for this tab, nothing else.
        }
    });

    const sectionItemClass = (isActive: boolean): string =>
        isActive
            ? 'text-indigo-700 cursor-default'
            : 'text-gray-500 hover:text-gray-700 cursor-pointer';
</script>

<div class="flex flex-col p-2">
    <div
        role="tablist"
        class="flex flex-row items-center space-x-8 border-b border-gray-300 pb-1.5"
    >
        {#each sections as section (section.id)}
            <div class="tab-item {sectionItemClass(activeSection === section.id)}">
                <button
                    type="button"
                    role="tab"
                    class="tab-button text-sm {activeSection === section.id ? '' : 'cursor-pointer'}"
                    aria-selected={activeSection === section.id}
                    aria-controls={`my-station-${section.id}`}
                    onclick={() => (activeSection = section.id)}>{section.title}</button
                >
            </div>
        {/each}
    </div>

    <div class="flex h-full">
        <div class="flex w-200">
        {#if activeSection === 'identity'}
            <div id="my-station-identity" role="tabpanel" class="flex flex-col space-y-1 pt-3">
                <div class="flex space-x-4">
                    <ValidatedInput
                        id="station-callsign"
                        label="Station Callsign"
                        bind:value={configState.loggingStation.stationCallsign}
                        validator={isValidCallsign}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="owner-callsign"
                        label="Owner's Callsign"
                        bind:value={configState.loggingStation.ownerCallsign}
                        validator={isValidCallsign}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                </div>
                <div class="flex space-x-4">
                    <ValidatedInput
                        id="operator"
                        label="Operator"
                        bind:value={configState.loggingStation.operator}
                        validator={isValidCallsign}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="my-name"
                        label="Operator Name"
                        bind:value={configState.loggingStation.myName}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                </div>
            </div>
        {:else if activeSection === 'location'}
            <div id="my-station-location" role="tabpanel" class="flex flex-col pt-3">
                <div class="flex space-x-4">
                    <ValidatedInput
                        id="my-gridsquare"
                        label="Grid Square"
                        bind:value={configState.loggingStation.myGridsquare}
                        validator={isValidMaidenhead}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="my-cq-zone"
                        label="CQ Zone"
                        bind:value={configState.loggingStation.myCqZone}
                        validator={isValidCqZone}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="my-itu-zone"
                        label="ITU Zone"
                        bind:value={configState.loggingStation.myItuZone}
                        validator={isValidItuZone}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="my-dxcc"
                        label="DXCC"
                        bind:value={configState.loggingStation.myDxcc}
                        validator={isValidDxcc}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                </div>
                <div class="flex space-x-4">
                    <ValidatedInput
                        id="my-street"
                        label="Street"
                        bind:value={configState.loggingStation.myStreet}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="my-city"
                        label="City"
                        bind:value={configState.loggingStation.myCity}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="my-postal-code"
                        label="Postal Code"
                        bind:value={configState.loggingStation.myPostalCode}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="my-country"
                        label="Country"
                        bind:value={configState.loggingStation.myCountry}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                </div>
                <div class="flex space-x-4">
                    <ValidatedInput
                        id="my-altitude"
                        label="Altitude (m)"
                        bind:value={configState.loggingStation.myAltitude}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <!-- Daemon-derived from Grid Square; read-only mirror. -->
                    <div class="w-38">
                        <span class="input-label">Latitude</span>
                        <p class="mt-2">{configState.loggingStation.myLat || '—'}</p>
                    </div>
                    <div class="w-38">
                        <span class="input-label">Longitude</span>
                        <p class="mt-2">{configState.loggingStation.myLon || '—'}</p>
                    </div>
                </div>
            </div>
        {:else if activeSection === 'equipment'}
            <div id="my-station-equipment" role="tabpanel" class="flex flex-col space-y-3 pt-3">
                <div class="flex space-x-4">
                    <!--
                        Rig field: when CAT is live the rigdef's
                        human-readable name (e.g. "Yaesu FTdx10",
                        resolved daemon-side from bridge.cat.driver)
                        wins and the field is read-only. When CAT is
                        off the operator's typed value is editable and
                        round-trips via PUT /v1/config. The two
                        branches keep the visual layout identical;
                        only the data source and readonly state differ.
                    -->
                    {#if displayedState.isLive}
                        <ValidatedInput
                            id="my-rig"
                            label="Rig"
                            value={displayedState.rigName}
                            validator={passthrough}
                            widthClass="w-fit"
                            inputClass="w-38 bg-surface-disabled cursor-default"
                            readonly
                            title="From CAT — change driver via config.json"
                        />
                    {:else}
                        <ValidatedInput
                            id="my-rig"
                            label="Rig"
                            bind:value={configState.loggingStation.myRig}
                            validator={passthrough}
                            widthClass="w-fit"
                            inputClass="w-38"
                        />
                    {/if}
                    <ValidatedInput
                        id="my-antenna"
                        label="Antenna"
                        bind:value={configState.loggingStation.myAntenna}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />

                    <!--
                        Default TX power: CAT-reported power wins when
                        the bridge is live (read-only display, no
                        round-trip — the live value is transient). When
                        CAT is off the operator's persisted default
                        feeds ADIF TX_PWR; 0 means "not set" → TX_PWR
                        omitted from the QSO record. Persisted in
                        config.json via the station block.
                    -->
                    <div class="flex flex-col w-64">
                        <label for="default-power" class="input-label">Default TX power (W)</label>
                        <input
                            id="default-power"
                            type="number"
                            step="1"
                            min="0"
                            max="2000"
                            class="input-base w-38 mt-1 {displayedState.isLive
                                ? 'bg-surface-disabled cursor-default'
                                : ''}"
                            value={displayedState.isLive
                                ? catState.power
                                : configState.station.defaultPower}
                            readonly={displayedState.isLive}
                            title={displayedState.isLive ? 'From CAT (PC)' : ''}
                            oninput={(e) => {
                                if (!displayedState.isLive) {
                                    configState.station.defaultPower = Number(
                                        e.currentTarget.value,
                                    );
                                }
                            }}
                        />
                        <p class="text-xs opacity-70 mt-1 max-w-md">
                            Used only when CAT is unavailable. When CAT is connected, the rig's
                            reported power overrides this. Set to 0 to omit ADIF TX_PWR from QSO
                            records.
                        </p>
                    </div>
                </div>
            </div>
        {:else if activeSection === 'modes'}
            <div class="flex w-full p-2">
                <div class="flex flex-col w-110 overflow-hidden">
                    <ul role="list" class="flex flex-col space-y-3 h-52 p-2 overflow-y-scroll">
                        {#each configState.bridge.rigModes as rigStr (rigStr)}
                            {#if editingModes[rigStr]}
                        <li class="flex items-center space-x-4 text-sm border border-gray-300 rounded-md px-6 py-3">
                            <div class="w-22 font-semibold">{rigStr}</div>
                            <span>:</span>
                            <div>
                                <input
                                    id={`mode-map-${rigStr}-mode`}
                                    type="text"
                                    class="input-base w-30"
                                    placeholder="MODE"
                                    bind:value={editingModes[rigStr].mode}
                                    autocomplete="off"
                                    spellcheck="false"
                                />
                            </div>
                            <div>
                                <input
                                    id={`mode-map-${rigStr}-submode`}
                                    type="text"
                                    class="input-base w-30"
                                    placeholder="(optional)"
                                    bind:value={editingModes[rigStr].submode}
                                    autocomplete="off"
                                    spellcheck="false"
                                />
                            </div>
                        </li>
                            {/if}
                        {/each}
                    </ul>
                </div>
                <div class="flex-col p-4 w-80">
                    {#if configState.bridge.rigModes.length === 0}
                        <p class="text-sm opacity-70">
                            No CAT rig is configured. Mode mappings translate a rig defined MODE
                            into an ADIF MODE / SUBMODE values; the table populates once
                            <code>bridge.enabled</code> is set in <code>config.json</code> and the
                            application recognises the configured driver.
                        </p>
                    {:else}
                    <p class="text-xs opacity-70">
                        Translation table for <strong>{configState.station.rigName}</strong>: each
                        row maps a rig defined MODE (left) to an ADIF MODE (centre) plus an optional
                        ADIF SUBMODE (right). The defaults are sensible for common usage; change
                        any row when you're operating a different protocol (e.g. running PSK31 on
                        DATA-U instead of FT8). Empty MODE clears the operator override so the
                        default mappings applies; validation rejects unknown ADIF values with an
                        error message.
                    </p>
                    {/if}
                </div>
            </div>
        {:else if activeSection === 'cw'}
            <div id="my-station-cw" role="tabpanel" class="flex flex-col space-y-1 pt-3">
                <div class="flex space-x-4">
                    <ValidatedInput
                        id="my-morse-key-type"
                        label="Morse Key Type"
                        bind:value={configState.loggingStation.myMorseKeyType}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                    <ValidatedInput
                        id="my-morse-key-info"
                        label="Morse Key Info"
                        bind:value={configState.loggingStation.myMorseKeyInfo}
                        validator={passthrough}
                        widthClass="w-fit"
                        inputClass="w-38"
                    />
                </div>
            </div>
        {:else if activeSection === 'qso'}
            <div id="my-station-qso" role="tabpanel" class="flex flex-row space-x-4 pt-3">
                <div class="flex flex-col space-y-4">
                    <h4 class="text-xs font-semibold uppercase tracking-wide opacity-70">Misc</h4>
                    <!--
                    QSO_RANDOM tri-state: 'off' omits the field from every
                    ADIF record (default); 'Y' / 'N' force the value on
                    every QSO. localStorage-persisted via qsoDefaults.
                -->
                    <div class="flex flex-col">
                        <label for="qso-random" class="input-label">QSO Random</label>
                        <select
                            id="qso-random"
                            class="input-base w-38 mt-1"
                            bind:value={qsoDefaults.qsoRandom}
                        >
                            <option value="off">Don't emit</option>
                            <option value="Y">Y (random)</option>
                            <option value="N">N (scheduled)</option>
                        </select>
                    </div>

                    <!--
                    Linear-amp pair. Daemon-persisted (config.json
                    station block); applied to TX power on QSO submit
                    via displayedState.effectivePower when ampEnabled is
                    true. Multiplier is meaningful only with the toggle on.
                -->
                    <div class="flex items-center space-x-2 w-56">
                        <input
                            id="amp-enabled"
                            type="checkbox"
                            bind:checked={configState.station.ampEnabled}
                        />
                        <label for="amp-enabled" class="text-sm">Use linear amp multiplier</label>
                    </div>
                    <div class="flex flex-col">
                        <label for="amp-multiplier" class="input-label">Amp multiplier</label>
                        <input
                            id="amp-multiplier"
                            type="number"
                            step="1"
                            min="0"
                            max="1000"
                            class="input-base w-fit mt-1"
                            bind:value={configState.station.ampMultiplier}
                            disabled={!configState.station.ampEnabled}
                        />
                    </div>
                </div>
                <div class="flex flex-col space-y-4">
                    <!--
                    Notification toggles. Errors / duplicates always toast
                    regardless of these flags — see qsoDefaults.svelte.ts
                    for the rationale. Mute the chatty info toasts here
                    when they become noise during a high-rate run.
                -->
                    <div class="flex flex-col space-y-1 w-56">
                        <h4 class="text-xs font-semibold uppercase tracking-wide opacity-70">
                            Notifications
                        </h4>
                        <div class="flex items-center space-x-2">
                            <input
                                id="notify-qso-stored"
                                type="checkbox"
                                bind:checked={qsoDefaults.notifyQsoStored}
                            />
                            <label for="notify-qso-stored" class="text-sm"
                                >Toast on QSO stored</label
                            >
                        </div>
                        <div class="flex items-center space-x-2">
                            <input
                                id="notify-config-saved"
                                type="checkbox"
                                bind:checked={qsoDefaults.notifyConfigSaved}
                            />
                            <label for="notify-config-saved" class="text-sm"
                                >Toast on My Station updated</label
                            >
                        </div>
                    </div>
                </div>
            </div>
        {/if}
        </div>
        <!--
        Update button sits outside the tab content so it persists across
        section switches and saves the panel as a whole. PUT roundtrips
        the full logging_station block; the daemon's response is
        re-applied so derived fields (my_lat / my_lon) and normalised
        forms (canonical gridsquare casing) flow back into the UI.
    -->
        <div class="flex w-32 h-52 justify-end items-end">
            <button
                id="my-station-update-btn"
                type="button"
                onclick={onUpdate}
                disabled={saving}
                class="h-9 cursor-pointer rounded-md bg-focus px-4 py-1.5 text-base font-semibold text-white shadow-sm hover:bg-focus-ring focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-50 disabled:cursor-not-allowed"
                >{saving ? 'Saving…' : 'Update'}</button
            >
        </div>
    </div>
</div>
