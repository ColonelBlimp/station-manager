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

export namespace types {
	
	export class ListenerConfig {
	    name: string;
	    enabled: boolean;
	    host: string;
	    port: number;
	    protocol: string;
	    buffer_size: number;
	    log_payload: boolean;
	    handler?: string;
	    handler_config?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new ListenerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.protocol = source["protocol"];
	        this.buffer_size = source["buffer_size"];
	        this.log_payload = source["log_payload"];
	        this.handler = source["handler"];
	        this.handler_config = source["handler_config"];
	    }
	}
	export class OptionalConfigs {
	    qrz_view_url: string;
	
	    static createFrom(source: any = {}) {
	        return new OptionalConfigs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.qrz_view_url = source["qrz_view_url"];
	    }
	}
	export class LoggingStation {
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
	
	    static createFrom(source: any = {}) {
	        return new LoggingStation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	    }
	}
	export class EmailConfig {
	    name: string;
	    enabled: boolean;
	    username: string;
	    password: string;
	    host: string;
	    port: number;
	    from: string;
	    to: string;
	    subject: string;
	    body: string;
	    smtp_dial_timeout_sec?: number;
	    smtp_retry_count?: number;
	    smtp_retry_delay_sec?: number;
	
	    static createFrom(source: any = {}) {
	        return new EmailConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.subject = source["subject"];
	        this.body = source["body"];
	        this.smtp_dial_timeout_sec = source["smtp_dial_timeout_sec"];
	        this.smtp_retry_count = source["smtp_retry_count"];
	        this.smtp_retry_delay_sec = source["smtp_retry_delay_sec"];
	    }
	}
	export class ForwarderConfig {
	    name: string;
	    enabled: boolean;
	    url: string;
	    apikey?: string;
	    username?: string;
	    password?: string;
	    useragent: string;
	    timeout_sec: number;
	
	    static createFrom(source: any = {}) {
	        return new ForwarderConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.url = source["url"];
	        this.apikey = source["apikey"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.useragent = source["useragent"];
	        this.timeout_sec = source["timeout_sec"];
	    }
	}
	export class LookupConfig {
	    name: string;
	    enabled: boolean;
	    url: string;
	    username?: string;
	    password?: string;
	    useragent: string;
	    timeout_sec: number;
	    view_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new LookupConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.url = source["url"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.useragent = source["useragent"];
	        this.timeout_sec = source["timeout_sec"];
	        this.view_url = source["view_url"];
	    }
	}
	export class CatConfig {
	    Enabled: boolean;
	    ListenerRateLimiterIntervalMS: number;
	    ListenerReadTimeoutMS: number;
	    SendChannelSize: number;
	    ProcessingChannelSize: number;
	
	    static createFrom(source: any = {}) {
	        return new CatConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Enabled = source["Enabled"];
	        this.ListenerRateLimiterIntervalMS = source["ListenerRateLimiterIntervalMS"];
	        this.ListenerReadTimeoutMS = source["ListenerReadTimeoutMS"];
	        this.SendChannelSize = source["SendChannelSize"];
	        this.ProcessingChannelSize = source["ProcessingChannelSize"];
	    }
	}
	export class SerialConfig {
	    PortName: string;
	    BaudRate: number;
	    DataBits: number;
	    Parity: number;
	    StopBits: number;
	    ReadTimeoutMS: number;
	    WriteTimeoutMS: number;
	    RTS: boolean;
	    DTR: boolean;
	    LineDelimiter: number;
	
	    static createFrom(source: any = {}) {
	        return new SerialConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.PortName = source["PortName"];
	        this.BaudRate = source["BaudRate"];
	        this.DataBits = source["DataBits"];
	        this.Parity = source["Parity"];
	        this.StopBits = source["StopBits"];
	        this.ReadTimeoutMS = source["ReadTimeoutMS"];
	        this.WriteTimeoutMS = source["WriteTimeoutMS"];
	        this.RTS = source["RTS"];
	        this.DTR = source["DTR"];
	        this.LineDelimiter = source["LineDelimiter"];
	    }
	}
	export class ValueMapping {
	    Key: string;
	    Value: string;
	
	    static createFrom(source: any = {}) {
	        return new ValueMapping(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Key = source["Key"];
	        this.Value = source["Value"];
	    }
	}
	export class Marker {
	    Tag: string;
	    Index: number;
	    Length: number;
	    ValueMappings: ValueMapping[];
	
	    static createFrom(source: any = {}) {
	        return new Marker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Tag = source["Tag"];
	        this.Index = source["Index"];
	        this.Length = source["Length"];
	        this.ValueMappings = this.convertValues(source["ValueMappings"], ValueMapping);
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
	export class CatState {
	    Prefix: string;
	    Markers: Marker[];
	    Data: string;
	
	    static createFrom(source: any = {}) {
	        return new CatState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Prefix = source["Prefix"];
	        this.Markers = this.convertValues(source["Markers"], Marker);
	        this.Data = source["Data"];
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
	export class CatCommand {
	    Name: string;
	    Cmd: string;
	
	    static createFrom(source: any = {}) {
	        return new CatCommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Cmd = source["Cmd"];
	    }
	}
	export class RigConfig {
	    ID: number;
	    Name: string;
	    Model: string;
	    Terminator: string;
	    CatCommands: CatCommand[];
	    CatStates: CatState[];
	    SerialConfig: SerialConfig;
	    CatConfig: CatConfig;
	
	    static createFrom(source: any = {}) {
	        return new RigConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Model = source["Model"];
	        this.Terminator = source["Terminator"];
	        this.CatCommands = this.convertValues(source["CatCommands"], CatCommand);
	        this.CatStates = this.convertValues(source["CatStates"], CatState);
	        this.SerialConfig = this.convertValues(source["SerialConfig"], SerialConfig);
	        this.CatConfig = this.convertValues(source["CatConfig"], CatConfig);
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
	export class ServerConfig {
	    name: string;
	    host: string;
	    port: number;
	    tls_enabled: boolean;
	    tls_cert_file: string;
	    tls_key_file: string;
	    read_timeout: number;
	    write_timeout: number;
	    idle_timeout: number;
	    body_limit: number;
	
	    static createFrom(source: any = {}) {
	        return new ServerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.tls_enabled = source["tls_enabled"];
	        this.tls_cert_file = source["tls_cert_file"];
	        this.tls_key_file = source["tls_key_file"];
	        this.read_timeout = source["read_timeout"];
	        this.write_timeout = source["write_timeout"];
	        this.idle_timeout = source["idle_timeout"];
	        this.body_limit = source["body_limit"];
	    }
	}
	export class RequiredConfigs {
	    setup_complete: boolean;
	    default_logbook_id: number;
	    default_rig_id: number;
	    default_freq: string;
	    default_mode: string;
	    default_is_random_qso: boolean;
	    power_multiplier: number;
	    default_tx_power: number;
	    use_power_multiplier: boolean;
	    default_fwd_email: string;
	    qso_forwarding_poll_interval_sec: number;
	    qso_forwarding_worker_count: number;
	    qso_forwarding_queue_size: number;
	    qso_forwarding_row_limit: number;
	    database_write_queue_size: number;
	    pagination_page_size: number;
	
	    static createFrom(source: any = {}) {
	        return new RequiredConfigs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.setup_complete = source["setup_complete"];
	        this.default_logbook_id = source["default_logbook_id"];
	        this.default_rig_id = source["default_rig_id"];
	        this.default_freq = source["default_freq"];
	        this.default_mode = source["default_mode"];
	        this.default_is_random_qso = source["default_is_random_qso"];
	        this.power_multiplier = source["power_multiplier"];
	        this.default_tx_power = source["default_tx_power"];
	        this.use_power_multiplier = source["use_power_multiplier"];
	        this.default_fwd_email = source["default_fwd_email"];
	        this.qso_forwarding_poll_interval_sec = source["qso_forwarding_poll_interval_sec"];
	        this.qso_forwarding_worker_count = source["qso_forwarding_worker_count"];
	        this.qso_forwarding_queue_size = source["qso_forwarding_queue_size"];
	        this.qso_forwarding_row_limit = source["qso_forwarding_row_limit"];
	        this.database_write_queue_size = source["database_write_queue_size"];
	        this.pagination_page_size = source["pagination_page_size"];
	    }
	}
	export class LoggingConfig {
	    level: string;
	    skip_frame_count: number;
	    with_timestamp: boolean;
	    console_logging: boolean;
	    file_logging: boolean;
	    rel_log_file_dir: string;
	    log_file_max_backups: number;
	    log_file_max_age_days: number;
	    log_file_max_size_mb: number;
	    shutdown_timeout_ms: number;
	    shutdown_timeout_warning: boolean;
	    console_no_color: boolean;
	    console_time_format: string;
	    log_file_compress: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LoggingConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.skip_frame_count = source["skip_frame_count"];
	        this.with_timestamp = source["with_timestamp"];
	        this.console_logging = source["console_logging"];
	        this.file_logging = source["file_logging"];
	        this.rel_log_file_dir = source["rel_log_file_dir"];
	        this.log_file_max_backups = source["log_file_max_backups"];
	        this.log_file_max_age_days = source["log_file_max_age_days"];
	        this.log_file_max_size_mb = source["log_file_max_size_mb"];
	        this.shutdown_timeout_ms = source["shutdown_timeout_ms"];
	        this.shutdown_timeout_warning = source["shutdown_timeout_warning"];
	        this.console_no_color = source["console_no_color"];
	        this.console_time_format = source["console_time_format"];
	        this.log_file_compress = source["log_file_compress"];
	    }
	}
	export class DatastoreConfig {
	    driver: string;
	    path: string;
	    options: Record<string, string>;
	    host?: string;
	    port?: number;
	    user?: string;
	    pass?: string;
	    database?: string;
	    ssl_mode?: string;
	    max_open_conns: number;
	    max_idle_conns: number;
	    conn_max_lifetime: number;
	    conn_max_idle_time: number;
	    context_timeout: number;
	    transaction_context_timeout: number;
	    Debug: boolean;
	    params?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new DatastoreConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.driver = source["driver"];
	        this.path = source["path"];
	        this.options = source["options"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.pass = source["pass"];
	        this.database = source["database"];
	        this.ssl_mode = source["ssl_mode"];
	        this.max_open_conns = source["max_open_conns"];
	        this.max_idle_conns = source["max_idle_conns"];
	        this.conn_max_lifetime = source["conn_max_lifetime"];
	        this.conn_max_idle_time = source["conn_max_idle_time"];
	        this.context_timeout = source["context_timeout"];
	        this.transaction_context_timeout = source["transaction_context_timeout"];
	        this.Debug = source["Debug"];
	        this.params = source["params"];
	    }
	}
	export class AppConfig {
	    datastore_config: DatastoreConfig;
	    logging_config: LoggingConfig;
	    required_configs: RequiredConfigs;
	    server_config?: ServerConfig;
	    rig_configs?: RigConfig[];
	    lookup_service_configs?: LookupConfig[];
	    forwarding_configs?: ForwarderConfig[];
	    email_config: EmailConfig;
	    logging_station: LoggingStation;
	    optional_configs: OptionalConfigs;
	    listener_configs?: ListenerConfig[];
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.datastore_config = this.convertValues(source["datastore_config"], DatastoreConfig);
	        this.logging_config = this.convertValues(source["logging_config"], LoggingConfig);
	        this.required_configs = this.convertValues(source["required_configs"], RequiredConfigs);
	        this.server_config = this.convertValues(source["server_config"], ServerConfig);
	        this.rig_configs = this.convertValues(source["rig_configs"], RigConfig);
	        this.lookup_service_configs = this.convertValues(source["lookup_service_configs"], LookupConfig);
	        this.forwarding_configs = this.convertValues(source["forwarding_configs"], ForwarderConfig);
	        this.email_config = this.convertValues(source["email_config"], EmailConfig);
	        this.logging_station = this.convertValues(source["logging_station"], LoggingStation);
	        this.optional_configs = this.convertValues(source["optional_configs"], OptionalConfigs);
	        this.listener_configs = this.convertValues(source["listener_configs"], ListenerConfig);
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

