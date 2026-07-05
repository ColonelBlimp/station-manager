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
    import ValidatedInput from '../components/ValidatedInput.svelte';

    /*
        Update flow. This panel edits a LOCAL form snapshot, never
        configState directly — because configState mirrors the daemon's
        persisted view and `submitQso` reads the MY_* identity fields from
        it live at log time (see qsoDraft.svelte.ts). Binding the editors
        straight into configState let a half-typed grid, an abandoned
        edit, or a failed PUT contaminate the next logged QSO's
        MY_GRIDSQUARE / MY_RIG / TX_PWR — durable data forwarded to
        QRZ/ClubLog. So: edit `form`, PUT from `form`, and push into
        configState ONLY via applyResponse after the daemon confirms —
        then re-seed `form` so the daemon's normalisations land in the UI:
          - station_callsign upper-cased
          - my_gridsquare canonicalised (upper field, lower subsquare)
          - my_lat / my_lon derived from my_gridsquare
        my_lat / my_lon are NOT sent — daemon-derived; a wire write of
        stale values would just be wasted bytes. Fields with no editor
        here (morse keys, postal address, zones/DXCC — moved to the config
        SPA) are round-tripped unchanged from configState in the PUT so
        the daemon's full-replace of logging_station doesn't clear them;
        they can't be dirty here, so reading them from configState is safe.
    */
    interface StationForm {
        stationCallsign: string;
        ownerCallsign: string;
        operator: string;
        myName: string;
        myGridsquare: string;
        myAltitude: string;
        myRig: string;
        myAntenna: string;
        ampEnabled: boolean;
        ampMultiplier: number;
        defaultPower: number;
    }

    // Snapshot the editable fields off configState. Called at init and again
    // after a confirmed save (so the daemon's canonicalised values replace the
    // typed ones). configState is hydrated before the logging UI mounts, so the
    // init-time read sees real values.
    function seedForm(): StationForm {
        const ls = configState.loggingStation;
        const st = configState.station;
        return {
            stationCallsign: ls.stationCallsign,
            ownerCallsign: ls.ownerCallsign,
            operator: ls.operator,
            myName: ls.myName,
            myGridsquare: ls.myGridsquare,
            myAltitude: ls.myAltitude,
            myRig: ls.myRig,
            myAntenna: ls.myAntenna,
            ampEnabled: st.ampEnabled,
            ampMultiplier: st.ampMultiplier,
            defaultPower: st.defaultPower,
        };
    }

    let form = $state(seedForm());
    let saving = $state(false);

    async function onUpdate(): Promise<void> {
        if (saving) return;
        saving = true;
        try {
            // Pass-through fields with no editor here (morse keys, postal address,
            // zones, DXCC) come straight from configState — untouched by this panel,
            // so they're never dirty. The daemon full-replaces logging_station on
            // PUT, so omitting them would clear the stored values.
            const ls = configState.loggingStation;

            const outcome = await putConfig({
                logging_station: {
                    station_callsign: form.stationCallsign,
                    operator: form.operator,
                    owner_callsign: form.ownerCallsign,
                    my_name: form.myName,
                    my_gridsquare: form.myGridsquare,
                    my_street: ls.myStreet,
                    my_city: ls.myCity,
                    my_postal_code: ls.myPostalCode,
                    my_country: ls.myCountry,
                    my_altitude: form.myAltitude,
                    my_cq_zone: ls.myCqZone,
                    my_itu_zone: ls.myItuZone,
                    my_dxcc: ls.myDxcc,
                    my_rig: form.myRig,
                    my_antenna: form.myAntenna,
                    my_morse_key_type: ls.myMorseKeyType,
                    my_morse_key_info: ls.myMorseKeyInfo,
                },
                station: {
                    amp_enabled: form.ampEnabled,
                    amp_multiplier: form.ampMultiplier,
                    default_power: form.defaultPower,
                },
            });
            switch (outcome.kind) {
                case 'ok':
                    configState.applyResponse(outcome.config);
                    // Re-seed so the daemon's normalisations (upper-cased call,
                    // canonical grid, derived lat/lon) replace what was typed.
                    form = seedForm();
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
    type SectionId = 'identity' | 'location' | 'equipment' | 'qso';

    interface Section {
        id: SectionId;
        title: string;
    }

    const sections: Section[] = [
        { id: 'identity', title: 'Identity' },
        { id: 'location', title: 'Location' },
        { id: 'equipment', title: 'Equipment' },
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
    const VALID_SECTIONS: readonly SectionId[] = ['identity', 'location', 'equipment', 'qso'];

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

    /*
        WAI-ARIA tabs keyboard contract — same shape as InfoPanel.
        ArrowLeft/Right cycle (wrap), Home/End jump to ends, roving
        tabindex on the buttons. Auto-activation: focus and selection
        move together since the tab set is small and the operator
        always wants to see the section they're focusing.
    */
    function moveSection(delta: number): void {
        const idx = sections.findIndex((s) => s.id === activeSection);
        const next = (idx + delta + sections.length) % sections.length;
        activeSection = sections[next].id;
        document.getElementById(`my-station-tab-${sections[next].id}`)?.focus();
    }

    function handleSectionKeydown(e: KeyboardEvent): void {
        switch (e.key) {
            case 'ArrowRight':
                e.preventDefault();
                moveSection(1);
                break;
            case 'ArrowLeft':
                e.preventDefault();
                moveSection(-1);
                break;
            case 'Home':
                e.preventDefault();
                activeSection = sections[0].id;
                document.getElementById(`my-station-tab-${sections[0].id}`)?.focus();
                break;
            case 'End':
                e.preventDefault();
                activeSection = sections[sections.length - 1].id;
                document
                    .getElementById(`my-station-tab-${sections[sections.length - 1].id}`)
                    ?.focus();
                break;
        }
    }
</script>

<div class="flex flex-col p-2">
    <div
        role="tablist"
        class="flex flex-row items-center space-x-8 border-b border-gray-300 pb-1.5"
    >
        {#each sections as section (section.id)}
            <div class="tab-item {sectionItemClass(activeSection === section.id)}">
                <button
                    id={`my-station-tab-${section.id}`}
                    type="button"
                    role="tab"
                    class="tab-button text-sm {activeSection === section.id
                        ? ''
                        : 'cursor-pointer'}"
                    aria-selected={activeSection === section.id}
                    aria-controls={`my-station-${section.id}`}
                    tabindex={activeSection === section.id ? 0 : -1}
                    onclick={() => (activeSection = section.id)}
                    onkeydown={handleSectionKeydown}>{section.title}</button
                >
            </div>
        {/each}
    </div>

    <div class="flex h-full">
        <div class="flex w-198">
            {#if activeSection === 'identity'}
                <div
                    id="my-station-identity"
                    role="tabpanel"
                    aria-labelledby="my-station-tab-identity"
                    class="flex flex-col space-y-1 pt-3"
                >
                    <div class="flex space-x-4">
                        <ValidatedInput
                            id="station-callsign"
                            label="Station Callsign"
                            bind:value={form.stationCallsign}
                            validator={isValidCallsign}
                            widthClass="w-fit"
                            inputClass="w-38"
                        />
                        <ValidatedInput
                            id="owner-callsign"
                            label="Owner's Callsign"
                            bind:value={form.ownerCallsign}
                            validator={isValidCallsign}
                            widthClass="w-fit"
                            inputClass="w-38"
                        />
                    </div>
                    <div class="flex space-x-4">
                        <ValidatedInput
                            id="operator"
                            label="Operator"
                            bind:value={form.operator}
                            validator={isValidCallsign}
                            widthClass="w-fit"
                            inputClass="w-38"
                        />
                        <ValidatedInput
                            id="my-name"
                            label="Operator Name"
                            bind:value={form.myName}
                            validator={passthrough}
                            widthClass="w-fit"
                            inputClass="w-38"
                        />
                    </div>
                </div>
            {:else if activeSection === 'location'}
                <div
                    id="my-station-location"
                    role="tabpanel"
                    aria-labelledby="my-station-tab-location"
                    class="flex flex-col pt-3"
                >
                    <!--
                        Location holds only the operating-relevant geo fields:
                        Grid Square (operator-edited), Altitude, and the
                        daemon-derived Lat/Lon mirror. The set-once fields —
                        CQ/ITU zone, DXCC, and the postal address — moved to the
                        config SPA's Station tab (2026-06-25 LSPA cleanup); they're
                        still round-tripped unchanged in the PUT below (the daemon
                        full-replaces logging_station), so removing the editors
                        here doesn't clear them.
                    -->
                    <div class="flex space-x-4">
                        <ValidatedInput
                            id="my-gridsquare"
                            label="Grid Square"
                            bind:value={form.myGridsquare}
                            validator={isValidMaidenhead}
                            widthClass="w-fit"
                            inputClass="w-38"
                        />
                        <ValidatedInput
                            id="my-altitude"
                            label="Altitude (m)"
                            bind:value={form.myAltitude}
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
                <div
                    id="my-station-equipment"
                    role="tabpanel"
                    aria-labelledby="my-station-tab-equipment"
                    class="flex flex-col space-y-3 pt-3"
                >
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
                                bind:value={form.myRig}
                                validator={passthrough}
                                widthClass="w-fit"
                                inputClass="w-38"
                            />
                        {/if}
                        <ValidatedInput
                            id="my-antenna"
                            label="Antenna"
                            bind:value={form.myAntenna}
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
                            <label for="default-power" class="input-label"
                                >Default TX power (W)</label
                            >
                            <input
                                id="default-power"
                                type="number"
                                step="1"
                                min="0"
                                max="2000"
                                class="input-base w-38 mt-1 {displayedState.isLive
                                    ? 'bg-surface-disabled cursor-default'
                                    : ''}"
                                value={displayedState.isLive ? catState.power : form.defaultPower}
                                readonly={displayedState.isLive}
                                title={displayedState.isLive ? 'From CAT (PC)' : ''}
                                oninput={(e) => {
                                    if (!displayedState.isLive) {
                                        form.defaultPower = Number(e.currentTarget.value);
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
            {:else if activeSection === 'qso'}
                <div
                    id="my-station-qso"
                    role="tabpanel"
                    aria-labelledby="my-station-tab-qso"
                    class="flex flex-row space-x-4 pt-3"
                >
                    <div class="flex flex-col space-y-4">
                        <h4 class="text-xs font-semibold uppercase tracking-wide opacity-70">
                            Misc
                        </h4>
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
                                bind:checked={form.ampEnabled}
                            />
                            <label for="amp-enabled" class="text-sm"
                                >Use linear amp multiplier</label
                            >
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
                                bind:value={form.ampMultiplier}
                                disabled={!form.ampEnabled}
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
