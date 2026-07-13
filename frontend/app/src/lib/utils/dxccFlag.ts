/**
 * Numeric DXCC entity → flag emoji, for surfaces that have the stored QSO but
 * no live enrichment (the logbook table). QSO rows carry the numeric ADIF
 * DXCC (SM populates it at log time; the 2026-07-13 backfill filled the
 * historical gap), which is the stable key — country NAMES vary by source
 * ("Fed. Rep. of Germany" vs "Germany") and ccode isn't stored on rows.
 *
 * The map covers every entity in the operator's logbook as of 2026-07-13
 * (165) — extend by adding a line when a new entity is worked. DXCC splits
 * collapse to their ISO parent by convention (England/Scotland/Wales/NI → GB,
 * Sardinia/Sicily → IT, European/Asiatic Russia + Kaliningrad → RU, …).
 * Unmapped → '' → no flag (decoration degrades, never breaks).
 */

import { ccodeToFlag } from './flag';

const DXCC_CCODE: Record<string, string> = {
    '1': 'CA', // Canada
    '6': 'US', // Alaska
    '12': 'AI', // Anguilla
    '13': 'AQ', // Antarctica
    '14': 'AM', // Armenia
    '15': 'RU', // Asiatic Russia
    '21': 'ES', // Balearic Islands
    '24': 'BV', // Bouvet Island
    '27': 'BY', // Belarus
    '29': 'ES', // Canary Islands
    '32': 'ES', // Ceuta and Melilla
    '38': 'CC', // Cocos-Keeling Islands
    '40': 'GR', // Crete
    '45': 'GR', // Dodecanese
    '46': 'MY', // East Malaysia
    '48': 'KI', // East Kiribati
    '50': 'MX', // Mexico
    '52': 'EE', // Estonia
    '53': 'ET', // Ethiopia
    '54': 'RU', // European Russia
    '62': 'BB', // Barbados
    '66': 'BZ', // Belize
    '69': 'KY', // Cayman Islands
    '70': 'CU', // Cuba
    '72': 'DO', // Dominican Republic
    '74': 'SV', // El Salvador
    '75': 'GE', // Georgia
    '78': 'HT', // Haiti
    '82': 'JM', // Jamaica
    '88': 'PA', // Panama
    '90': 'TT', // Trinidad and Tobago
    '94': 'AG', // Antigua and Barbuda
    '95': 'DM', // Dominica
    '96': 'MS', // Montserrat
    '97': 'LC', // St Lucia
    '100': 'AR', // Argentina
    '108': 'BR', // Brazil
    '109': 'GW', // Guinea-Bissau
    '110': 'US', // Hawaii
    '112': 'CL', // Chile
    '114': 'IM', // Isle of Man
    '116': 'CO', // Colombia
    '126': 'RU', // Kaliningrad
    '130': 'KZ', // Kazakhstan
    '137': 'KR', // Republic of Korea
    '144': 'UY', // Uruguay
    '145': 'LV', // Latvia
    '146': 'LT', // Lithuania
    '147': 'AU', // Lord Howe Island
    '148': 'VE', // Venezuela
    '149': 'PT', // Azores
    '150': 'AU', // Australia
    '158': 'VU', // Vanuatu
    '159': 'MV', // Maldives
    '165': 'MU', // Mauritius
    '166': 'MP', // Mariana Islands
    '170': 'NZ', // New Zealand
    '179': 'MD', // Moldova
    '181': 'MZ', // Mozambique
    '190': 'WS', // Samoa
    '202': 'PR', // Puerto Rico
    '206': 'AT', // Austria
    '207': 'MU', // Rodriguez Island
    '209': 'BE', // Belgium
    '212': 'BG', // Bulgaria
    '214': 'FR', // Corsica
    '215': 'CY', // Cyprus
    '221': 'DK', // Denmark
    '222': 'FO', // Faroe Islands
    '223': 'GB', // England
    '224': 'FI', // Finland
    '225': 'IT', // Sardinia
    '227': 'FR', // France
    '230': 'DE', // Fed. Rep. of Germany
    '236': 'GR', // Greece
    '237': 'GL', // Greenland
    '239': 'HU', // Hungary
    '242': 'IS', // Iceland
    '245': 'IE', // Ireland
    '248': 'IT', // Italy (incl. Sicily)
    '249': 'KN', // St Kitts and Nevis
    '251': 'LI', // Liechtenstein
    '254': 'LU', // Luxembourg
    '256': 'PT', // Madeira Island
    '257': 'MT', // Malta
    '260': 'MC', // Monaco
    '263': 'NL', // Netherlands
    '265': 'GB', // Northern Ireland
    '266': 'NO', // Norway
    '269': 'PL', // Poland
    '272': 'PT', // Portugal
    '274': 'SH', // Tristan da Cunha and Gough Island
    '275': 'RO', // Romania
    '278': 'SM', // San Marino
    '279': 'GB', // Scotland
    '281': 'ES', // Spain
    '282': 'TV', // Tuvalu
    '284': 'SE', // Sweden
    '286': 'UG', // Uganda
    '287': 'CH', // Switzerland
    '288': 'UA', // Ukraine
    '291': 'US', // United States
    '292': 'UZ', // Uzbekistan
    '293': 'VN', // Vietnam
    '294': 'GB', // Wales
    '296': 'RS', // Serbia
    '299': 'MY', // West Malaysia
    '304': 'BH', // Bahrain
    '305': 'BD', // Bangladesh
    '308': 'CR', // Costa Rica
    '312': 'KH', // Cambodia
    '315': 'LK', // Sri Lanka
    '318': 'CN', // China
    '321': 'HK', // Hong Kong
    '324': 'IN', // India
    '327': 'ID', // Indonesia
    '336': 'IL', // Israel
    '339': 'JP', // Japan
    '342': 'JO', // Jordan
    '348': 'KW', // Kuwait
    '354': 'LB', // Lebanon
    '369': 'NP', // Nepal
    '370': 'OM', // Oman
    '375': 'PH', // Philippines
    '376': 'QA', // Qatar
    '378': 'SA', // Saudi Arabia
    '379': 'SC', // Seychelles
    '381': 'SG', // Singapore
    '384': 'SY', // Syria
    '386': 'TW', // Taiwan
    '387': 'TH', // Thailand
    '390': 'TR', // Turkey
    '391': 'AE', // United Arab Emirates
    '400': 'DZ', // Algeria
    '401': 'AO', // Angola
    '402': 'BW', // Botswana
    '406': 'CM', // Cameroon
    '408': 'CF', // Central African Republic
    '416': 'BJ', // Benin
    '420': 'GA', // Gabon
    '422': 'GM', // The Gambia
    '430': 'KE', // Kenya
    '432': 'LS', // Lesotho
    '438': 'MG', // Madagascar
    '440': 'MW', // Malawi
    '446': 'MA', // Morocco
    '450': 'NG', // Nigeria
    '452': 'ZW', // Zimbabwe
    '453': 'RE', // Reunion
    '462': 'ZA', // South Africa
    '464': 'NA', // Namibia
    '468': 'SZ', // Eswatini
    '482': 'ZM', // Zambia
    '497': 'HR', // Croatia
    '499': 'SI', // Slovenia
    '501': 'BA', // Bosnia and Herzegovina
    '502': 'MK', // North Macedonia
    '503': 'CZ', // Czech Republic
    '504': 'SK', // Slovak Republic
    '514': 'ME', // Montenegro
    '516': 'BL', // St. Barthelemy
    '517': 'CW', // Curacao
    '518': 'SX', // St. Maarten
    '520': 'BQ', // Bonaire
    '522': 'XK', // Republic of Kosovo
};

/** Flag emoji for a numeric ADIF DXCC entity code (string form, as stored on
 *  QSOs), or '' when unmapped/blank — the caller renders no flag. */
export function dxccFlag(dxcc: string | null | undefined): string {
    if (!dxcc) return '';
    return ccodeToFlag(DXCC_CCODE[dxcc.trim()] ?? '');
}
