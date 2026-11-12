// What the shell runs as, decided first (W-0027 D1). An installed app (Electron's app.isPackaged,
// true when the runtime binary is renamed station-manager-desktop) ignores every development
// selector: no test scenario, no profile override, no fake media. Only a strict false, a
// development run from a checkout, honours them; any other value is treated as installed.
export function startupMode({ isPackaged, env }) {
    if (isPackaged !== false) {
        return { scenario: '', automated: false, userData: null, fakeMedia: false };
    }
    const scenario = env.SM_DESKTOP_SCENARIO ?? '';
    const automated = scenario !== '';
    return {
        scenario,
        automated,
        // Automated runs use a throwaway profile unless told to share one (the second-launch
        // check); interactive runs use the default profile.
        userData: env.SM_DESKTOP_USERDATA || null,
        // Automated runs give Chromium a fake capture device, so a refused microphone request
        // is the permission policy's decision, never a missing device (codex-038).
        fakeMedia: automated,
    };
}
