export namespace config {
	
	export class Config {
	    Roots: string[];
	    ScanDepth: number;
	    Editor: string;
	    Terminal: string;
	    AutoFetchMinutes: number;
	    ShowNonGit: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Roots = source["Roots"];
	        this.ScanDepth = source["ScanDepth"];
	        this.Editor = source["Editor"];
	        this.Terminal = source["Terminal"];
	        this.AutoFetchMinutes = source["AutoFetchMinutes"];
	        this.ShowNonGit = source["ShowNonGit"];
	    }
	}

}

export namespace main {
	
	export class BranchInfo {
	    current: string;
	    all: string[];
	
	    static createFrom(source: any = {}) {
	        return new BranchInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.all = source["all"];
	    }
	}
	export class CommitView {
	    hash: string;
	    message: string;
	    author: string;
	    when: string;
	
	    static createFrom(source: any = {}) {
	        return new CommitView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.message = source["message"];
	        this.author = source["author"];
	        this.when = source["when"];
	    }
	}
	export class RepoView {
	    name: string;
	    path: string;
	    isGit: boolean;
	    branch: string;
	    dirty: boolean;
	    modified: number;
	    ahead: number;
	    behind: number;
	    hasUpstream: boolean;
	    remote: string;
	    dirtyFiles: string[];
	    lastHash: string;
	    lastMsg: string;
	    lastAuthor: string;
	    lastWhen: string;
	    language: string;
	    sizeBytes: number;
	    todo: number;
	    errMsg: string;
	    loaded: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RepoView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isGit = source["isGit"];
	        this.branch = source["branch"];
	        this.dirty = source["dirty"];
	        this.modified = source["modified"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.hasUpstream = source["hasUpstream"];
	        this.remote = source["remote"];
	        this.dirtyFiles = source["dirtyFiles"];
	        this.lastHash = source["lastHash"];
	        this.lastMsg = source["lastMsg"];
	        this.lastAuthor = source["lastAuthor"];
	        this.lastWhen = source["lastWhen"];
	        this.language = source["language"];
	        this.sizeBytes = source["sizeBytes"];
	        this.todo = source["todo"];
	        this.errMsg = source["errMsg"];
	        this.loaded = source["loaded"];
	    }
	}

}

