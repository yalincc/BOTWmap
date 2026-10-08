export namespace main {
	
	export class Config {
	    cemuSaveDir: string;
	    ryujinxSaveDir: string;
	    edenSaveDir: string;
	    emulator: string;
	    game: string;
	
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
	    }
	}

}

