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
	export class DayCountView {
	    date: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new DayCountView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.count = source["count"];
	    }
	}
	export class EdgeView {
	    id: string;
	    from: string;
	    to: string;
	    kind: string;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new EdgeView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.kind = source["kind"];
	        this.note = source["note"];
	    }
	}
	export class GitHubView {
	    ci: string;
	    prs: number;
	    issues: number;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GitHubView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ci = source["ci"];
	        this.prs = source["prs"];
	        this.issues = source["issues"];
	        this.available = source["available"];
	    }
	}
	export class GraphEdge {
	    from: string;
	    to: string;
	    manual: boolean;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new GraphEdge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = source["from"];
	        this.to = source["to"];
	        this.manual = source["manual"];
	        this.kind = source["kind"];
	    }
	}
	export class GraphNode {
	    id: string;
	    name: string;
	    tags: string[];
	    isGit: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GraphNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.tags = source["tags"];
	        this.isGit = source["isGit"];
	    }
	}
	export class GraphView {
	    nodes: GraphNode[];
	    edges: GraphEdge[];
	
	    static createFrom(source: any = {}) {
	        return new GraphView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodes = this.convertValues(source["nodes"], GraphNode);
	        this.edges = this.convertValues(source["edges"], GraphEdge);
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
	export class TaskView {
	    id: string;
	    title: string;
	    done: boolean;
	    status: string;
	    due: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.done = source["done"];
	        this.status = source["status"];
	        this.due = source["due"];
	    }
	}
	export class ProjectView {
	    id: string;
	    name: string;
	    type: string;
	    repoPath: string;
	    status: string;
	    priority: number;
	    deadline: string;
	    notes: string;
	    tags: string[];
	    tasks: TaskView[];
	    doneCount: number;
	    taskCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ProjectView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.repoPath = source["repoPath"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	        this.deadline = source["deadline"];
	        this.notes = source["notes"];
	        this.tags = source["tags"];
	        this.tasks = this.convertValues(source["tasks"], TaskView);
	        this.doneCount = source["doneCount"];
	        this.taskCount = source["taskCount"];
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
	export class SearchHit {
	    repo: string;
	    repoPath: string;
	    file: string;
	    line: number;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchHit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo = source["repo"];
	        this.repoPath = source["repoPath"];
	        this.file = source["file"];
	        this.line = source["line"];
	        this.text = source["text"];
	    }
	}
	export class SymbolsView {
	    goMainPkgs: string[];
	    goExported: string[];
	    npmScripts: string[];
	    npmBin: string[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SymbolsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.goMainPkgs = source["goMainPkgs"];
	        this.goExported = source["goExported"];
	        this.npmScripts = source["npmScripts"];
	        this.npmBin = source["npmBin"];
	        this.truncated = source["truncated"];
	    }
	}

}

