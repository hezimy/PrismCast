export namespace config {
	
	export interface Config {
	    device_name: string;
	    device_uuid: string;
	    player_path: string;
	    auto_start: boolean;
	    image_viewer_first: boolean;
	    volume: number;
	    language: string;
	    theme: string;
	    log_level: string;
	    verbose_log?: boolean;
	}

}

