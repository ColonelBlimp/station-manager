export namespace events {
	
	export enum EventName {
	    STATUS = "STATUS",
	}

}

export namespace iocdi {
	
	export class Container {
	
	
	    static createFrom(source: any = {}) {
	        return new Container(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace tags {
	
	export enum CatStateTag {
	    IDENTITY = "IDENTITY",
	    MAINMODE = "MAINMODE",
	    SELECT = "SELECT",
	    SPLIT = "SPLIT",
	    SUBMODE = "SUBMODE",
	    TXPWR = "TXPWR",
	    VFOAFREQ = "VFOAFREQ",
	    VFOBFREQ = "VFOBFREQ",
	}

}

export namespace types {
	
	export class ContactHistory {
	    id: number;
	    band: string;
	    freq: string;
	    mode: string;
	    qso_date: string;
	    time_on: string;
	    name: string;
	    country: string;
	    call: string;
	    rst_sent: string;
	    rst_rcvd: string;
	    notes: string;
	
	    static createFrom(source: any = {}) {
	        return new ContactHistory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.band = source["band"];
	        this.freq = source["freq"];
	        this.mode = source["mode"];
	        this.qso_date = source["qso_date"];
	        this.time_on = source["time_on"];
	        this.name = source["name"];
	        this.country = source["country"];
	        this.call = source["call"];
	        this.rst_sent = source["rst_sent"];
	        this.rst_rcvd = source["rst_rcvd"];
	        this.notes = source["notes"];
	    }
	}
	export class Country {
	    ID: number;
	    name: string;
	    prefix: string;
	    ccode: string;
	    continent: string;
	    cq_zone: string;
	    itu_zone: string;
	    dxcc_prefix: string;
	    time_offset: string;
	    short_path_distance: string;
	    long_path_distance: string;
	    short_path_bearing: string;
	    long_path_bearing: string;
	    is_new_entity: boolean;
	    local_time: string;
	
	    static createFrom(source: any = {}) {
	        return new Country(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.name = source["name"];
	        this.prefix = source["prefix"];
	        this.ccode = source["ccode"];
	        this.continent = source["continent"];
	        this.cq_zone = source["cq_zone"];
	        this.itu_zone = source["itu_zone"];
	        this.dxcc_prefix = source["dxcc_prefix"];
	        this.time_offset = source["time_offset"];
	        this.short_path_distance = source["short_path_distance"];
	        this.long_path_distance = source["long_path_distance"];
	        this.short_path_bearing = source["short_path_bearing"];
	        this.long_path_bearing = source["long_path_bearing"];
	        this.is_new_entity = source["is_new_entity"];
	        this.local_time = source["local_time"];
	    }
	}
	export class Logbook {
	    id: number;
	    user_id?: number;
	    name: string;
	    callsign: string;
	    api_key?: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new Logbook(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.user_id = source["user_id"];
	        this.name = source["name"];
	        this.callsign = source["callsign"];
	        this.api_key = source["api_key"];
	        this.description = source["description"];
	    }
	}
	export class Qso {
	    id: number;
	    logbook_id: number;
	    session_id: number;
	    sm_qso_upload_date: string;
	    sm_qso_upload_status: string;
	    sm_fwrd_by_email_date: string;
	    sm_fwrd_by_email_status: string;
	    qrzcom_qso_upload_date: string;
	    qrzcom_qso_upload_status: string;
	    a_index: string;
	    ant_path: string;
	    band?: string;
	    band_rx: string;
	    comment: string;
	    contest_id: string;
	    distance: string;
	    freq?: string;
	    freq_rx: string;
	    mode?: string;
	    submode: string;
	    notes: string;
	    qso_date?: string;
	    qso_date_off: string;
	    qso_random: string;
	    qso_complete: string;
	    rst_rcvd?: string;
	    rst_sent?: string;
	    rx_pwr: string;
	    srx: string;
	    stx: string;
	    time_off?: string;
	    time_on?: string;
	    tx_pwr: string;
	    rig: string;
	    csid: number;
	    address: string;
	    age: string;
	    altitude: string;
	    call?: string;
	    cont: string;
	    contacted_op: string;
	    country?: string;
	    cqz: string;
	    dxcc: string;
	    email: string;
	    eq_call: string;
	    gridsquare: string;
	    iota: string;
	    iota_island_id: string;
	    ituz: string;
	    lat: string;
	    lon: string;
	    name: string;
	    qth: string;
	    sig: string;
	    sig_info: string;
	    web: string;
	    wwff_ref: string;
	    ant_az?: string;
	    my_altitude: string;
	    my_antenna: string;
	    my_city: string;
	    my_country: string;
	    my_cq_zone: string;
	    my_dxcc: string;
	    my_gridsquare: string;
	    my_iota: string;
	    my_iota_island_id: string;
	    my_itu_zone: string;
	    my_lat: string;
	    my_lon: string;
	    my_morse_key_info: string;
	    my_morse_key_type: string;
	    my_name: string;
	    my_postal_code: string;
	    my_rig: string;
	    my_sig: string;
	    my_sig_info: string;
	    my_street: string;
	    my_wwff_ref: string;
	    operator: string;
	    owner_callsign: string;
	    station_callsign: string;
	    qslmsg: string;
	    qslmsg_rcvd: string;
	    qslrdate: string;
	    qslsdate: string;
	    qsl_rcvd: string;
	    qsl_rcvd_via: string;
	    qsl_rcvd_notes: string;
	    qsl_sent: string;
	    qsl_sent_via: string;
	    qsl_via: string;
	    country_details: Country;
	    contact_history: ContactHistory[];
	
	    static createFrom(source: any = {}) {
	        return new Qso(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.logbook_id = source["logbook_id"];
	        this.session_id = source["session_id"];
	        this.sm_qso_upload_date = source["sm_qso_upload_date"];
	        this.sm_qso_upload_status = source["sm_qso_upload_status"];
	        this.sm_fwrd_by_email_date = source["sm_fwrd_by_email_date"];
	        this.sm_fwrd_by_email_status = source["sm_fwrd_by_email_status"];
	        this.qrzcom_qso_upload_date = source["qrzcom_qso_upload_date"];
	        this.qrzcom_qso_upload_status = source["qrzcom_qso_upload_status"];
	        this.a_index = source["a_index"];
	        this.ant_path = source["ant_path"];
	        this.band = source["band"];
	        this.band_rx = source["band_rx"];
	        this.comment = source["comment"];
	        this.contest_id = source["contest_id"];
	        this.distance = source["distance"];
	        this.freq = source["freq"];
	        this.freq_rx = source["freq_rx"];
	        this.mode = source["mode"];
	        this.submode = source["submode"];
	        this.notes = source["notes"];
	        this.qso_date = source["qso_date"];
	        this.qso_date_off = source["qso_date_off"];
	        this.qso_random = source["qso_random"];
	        this.qso_complete = source["qso_complete"];
	        this.rst_rcvd = source["rst_rcvd"];
	        this.rst_sent = source["rst_sent"];
	        this.rx_pwr = source["rx_pwr"];
	        this.srx = source["srx"];
	        this.stx = source["stx"];
	        this.time_off = source["time_off"];
	        this.time_on = source["time_on"];
	        this.tx_pwr = source["tx_pwr"];
	        this.rig = source["rig"];
	        this.csid = source["csid"];
	        this.address = source["address"];
	        this.age = source["age"];
	        this.altitude = source["altitude"];
	        this.call = source["call"];
	        this.cont = source["cont"];
	        this.contacted_op = source["contacted_op"];
	        this.country = source["country"];
	        this.cqz = source["cqz"];
	        this.dxcc = source["dxcc"];
	        this.email = source["email"];
	        this.eq_call = source["eq_call"];
	        this.gridsquare = source["gridsquare"];
	        this.iota = source["iota"];
	        this.iota_island_id = source["iota_island_id"];
	        this.ituz = source["ituz"];
	        this.lat = source["lat"];
	        this.lon = source["lon"];
	        this.name = source["name"];
	        this.qth = source["qth"];
	        this.sig = source["sig"];
	        this.sig_info = source["sig_info"];
	        this.web = source["web"];
	        this.wwff_ref = source["wwff_ref"];
	        this.ant_az = source["ant_az"];
	        this.my_altitude = source["my_altitude"];
	        this.my_antenna = source["my_antenna"];
	        this.my_city = source["my_city"];
	        this.my_country = source["my_country"];
	        this.my_cq_zone = source["my_cq_zone"];
	        this.my_dxcc = source["my_dxcc"];
	        this.my_gridsquare = source["my_gridsquare"];
	        this.my_iota = source["my_iota"];
	        this.my_iota_island_id = source["my_iota_island_id"];
	        this.my_itu_zone = source["my_itu_zone"];
	        this.my_lat = source["my_lat"];
	        this.my_lon = source["my_lon"];
	        this.my_morse_key_info = source["my_morse_key_info"];
	        this.my_morse_key_type = source["my_morse_key_type"];
	        this.my_name = source["my_name"];
	        this.my_postal_code = source["my_postal_code"];
	        this.my_rig = source["my_rig"];
	        this.my_sig = source["my_sig"];
	        this.my_sig_info = source["my_sig_info"];
	        this.my_street = source["my_street"];
	        this.my_wwff_ref = source["my_wwff_ref"];
	        this.operator = source["operator"];
	        this.owner_callsign = source["owner_callsign"];
	        this.station_callsign = source["station_callsign"];
	        this.qslmsg = source["qslmsg"];
	        this.qslmsg_rcvd = source["qslmsg_rcvd"];
	        this.qslrdate = source["qslrdate"];
	        this.qslsdate = source["qslsdate"];
	        this.qsl_rcvd = source["qsl_rcvd"];
	        this.qsl_rcvd_via = source["qsl_rcvd_via"];
	        this.qsl_rcvd_notes = source["qsl_rcvd_notes"];
	        this.qsl_sent = source["qsl_sent"];
	        this.qsl_sent_via = source["qsl_sent_via"];
	        this.qsl_via = source["qsl_via"];
	        this.country_details = this.convertValues(source["country_details"], Country);
	        this.contact_history = this.convertValues(source["contact_history"], ContactHistory);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UiConfig {
	    default_rig_id: number;
	    logbook: Logbook;
	    rig_name: string;
	    default_freq: string;
	    default_mode: string;
	    default_is_random_qso: boolean;
	    use_power_multiplier: boolean;
	    power_multiplier: number;
	    default_tx_power: number;
	    default_fwd_email: string;
	    owner_callsign: string;
	    pagination_page_size: number;
	    qrz_view_url: string;
	
	    static createFrom(source: any = {}) {
	        return new UiConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default_rig_id = source["default_rig_id"];
	        this.logbook = this.convertValues(source["logbook"], Logbook);
	        this.rig_name = source["rig_name"];
	        this.default_freq = source["default_freq"];
	        this.default_mode = source["default_mode"];
	        this.default_is_random_qso = source["default_is_random_qso"];
	        this.use_power_multiplier = source["use_power_multiplier"];
	        this.power_multiplier = source["power_multiplier"];
	        this.default_tx_power = source["default_tx_power"];
	        this.default_fwd_email = source["default_fwd_email"];
	        this.owner_callsign = source["owner_callsign"];
	        this.pagination_page_size = source["pagination_page_size"];
	        this.qrz_view_url = source["qrz_view_url"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

