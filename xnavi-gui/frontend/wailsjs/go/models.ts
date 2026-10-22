export namespace main {
	
	export class Config {
	    cemuSaveDir: string;
	    ryujinxSaveDir: string;
	    edenSaveDir: string;
	    emulator: string;
	    game: string;
	    tls: boolean;
	    autoOpenMap?: boolean;
	    autoDetectSave?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cemuSaveDir = source["cemuSaveDir"];
	        this.ryujinxSaveDir = source["ryujinxSaveDir"];
	        this.edenSaveDir = source["edenSaveDir"];
	        this.emulator = source["emulator"];
	        this.game = source["game"];
	        this.tls = source["tls"];
	        this.autoOpenMap = source["autoOpenMap"];
	        this.autoDetectSave = source["autoDetectSave"];
	    }
	}

}

