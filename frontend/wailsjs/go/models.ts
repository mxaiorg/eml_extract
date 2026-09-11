export namespace main {
	
	export class ExtractOptions {
	    paths: string[];
	    outputDir: string;
	    includeNameless: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExtractOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.paths = source["paths"];
	        this.outputDir = source["outputDir"];
	        this.includeNameless = source["includeNameless"];
	    }
	}
	export class Status {
	    ripmimeFound: boolean;
	    ripmimePath: string;
	    ripmimeVersion: string;
	    defaultOutputDir: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ripmimeFound = source["ripmimeFound"];
	        this.ripmimePath = source["ripmimePath"];
	        this.ripmimeVersion = source["ripmimeVersion"];
	        this.defaultOutputDir = source["defaultOutputDir"];
	    }
	}

}

