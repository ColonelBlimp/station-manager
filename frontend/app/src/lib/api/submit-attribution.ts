// GET /v1/submit-attribution (ADR 0085): the MY_RIG, effective OPERATOR and
// MY_NAME a Phone / CW submit is stored with right now. A saved draft records
// it, so a recovered submit can state the attribution it was made with. Read
// strictly: every field must be a string ("" is a known-empty value); anything
// else is a failed read (null), so a draft records MISSING attribution rather
// than a guess.

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export interface SubmitAttribution {
    myRig: string;
    operator: string;
    myName: string;
}

/** `operator` is the OPERATOR this page's submit sends — always sent, even
 *  empty, so the answer is resolved exactly as that submit would be. */
export async function fetchSubmitAttribution(
    operator: string,
    signal?: AbortSignal
): Promise<SubmitAttribution | null> {
    const url = `/v1/submit-attribution?operator=${encodeURIComponent(operator)}`;
    const fetched = await safeFetch(url, { method: 'GET', signal });
    if (!fetched.ok || !fetched.response.ok) return null;
    const body = await readJsonBody(fetched.response);
    if (
        !isPlainObject(body) ||
        typeof body.my_rig !== 'string' ||
        typeof body.operator !== 'string' ||
        typeof body.my_name !== 'string'
    ) {
        return null;
    }
    return { myRig: body.my_rig, operator: body.operator, myName: body.my_name };
}
