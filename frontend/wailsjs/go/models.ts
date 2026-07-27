export namespace config {
	
	export class Config {
	    Roots: string[];
	    ScanDepth: number;
	    Editor: string;
	    Terminal: string;
	    AutoFetchMinutes: number;
	    ShowNonGit: boolean;
	    AIProvider: string;
	    AIModel: string;
	    OpenAIKey: string;
	    GeminiKey: string;
	    NotionToken: string;
	    NotionTasksDB: string;
	
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
	        this.AIProvider = source["AIProvider"];
	        this.AIModel = source["AIModel"];
	        this.OpenAIKey = source["OpenAIKey"];
	        this.GeminiKey = source["GeminiKey"];
	        this.NotionToken = source["NotionToken"];
	        this.NotionTasksDB = source["NotionTasksDB"];
	    }
	}

}

export namespace git {
	
	export class RebaseAction {
	    hash: string;
	    op: string;
	
	    static createFrom(source: any = {}) {
	        return new RebaseAction(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.op = source["op"];
	    }
	}

}

export namespace intel {
	
	export class Brief {
	    text: string;
	    at: string;
	    lang: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Brief(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.at = source["at"];
	        this.lang = source["lang"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Turn {
	    role: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new Turn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.text = source["text"];
	    }
	}

}

export namespace main {
	
	export class AgendaItem {
	    projectId: string;
	    projectName: string;
	    kind: string;
	    taskId: string;
	    title: string;
	    due: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new AgendaItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.projectName = source["projectName"];
	        this.kind = source["kind"];
	        this.taskId = source["taskId"];
	        this.title = source["title"];
	        this.due = source["due"];
	        this.status = source["status"];
	    }
	}
	export class AuthStatusView {
	    signedIn: boolean;
	    login: string;
	    avatarUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.signedIn = source["signedIn"];
	        this.login = source["login"];
	        this.avatarUrl = source["avatarUrl"];
	    }
	}
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
	export class ConflictView {
	    localId: string;
	    name: string;
	    when: string;
	
	    static createFrom(source: any = {}) {
	        return new ConflictView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.localId = source["localId"];
	        this.name = source["name"];
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
	export class EditorOption {
	    name: string;
	    command: string;
	    installed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EditorOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
	        this.installed = source["installed"];
	    }
	}
	export class FileHit {
	    repo: string;
	    repoPath: string;
	    file: string;
	
	    static createFrom(source: any = {}) {
	        return new FileHit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo = source["repo"];
	        this.repoPath = source["repoPath"];
	        this.file = source["file"];
	    }
	}
	export class GitConflictView {
	    path: string;
	    kind: string;
	    mode: string;
	    mineLabel: string;
	    incomingLabel: string;
	
	    static createFrom(source: any = {}) {
	        return new GitConflictView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.mode = source["mode"];
	        this.mineLabel = source["mineLabel"];
	        this.incomingLabel = source["incomingLabel"];
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
	export class HealthIssue {
	    scope: string;
	    path: string;
	    error: string;
	    frozen: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HealthIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.path = source["path"];
	        this.error = source["error"];
	        this.frozen = source["frozen"];
	    }
	}
	export class ImportSummary {
	    path: string;
	    projects: number;
	    projectsOverwrite: number;
	    chats: number;
	    chatsOverwrite: number;
	    brief: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.projects = source["projects"];
	        this.projectsOverwrite = source["projectsOverwrite"];
	        this.chats = source["chats"];
	        this.chatsOverwrite = source["chatsOverwrite"];
	        this.brief = source["brief"];
	        this.error = source["error"];
	    }
	}
	export class NotionDBView {
	    id: string;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new NotionDBView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	    }
	}
	export class NotionDBList {
	    dbs: NotionDBView[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new NotionDBList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dbs = this.convertValues(source["dbs"], NotionDBView);
	        this.error = source["error"];
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
	
	export class NotionTaskView {
	    id: string;
	    title: string;
	    due: string;
	    status: string;
	    done: boolean;
	    url: string;
	    checkboxProp: string;
	
	    static createFrom(source: any = {}) {
	        return new NotionTaskView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.due = source["due"];
	        this.status = source["status"];
	        this.done = source["done"];
	        this.url = source["url"];
	        this.checkboxProp = source["checkboxProp"];
	    }
	}
	export class NotionResult {
	    tasks: NotionTaskView[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new NotionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tasks = this.convertValues(source["tasks"], NotionTaskView);
	        this.error = source["error"];
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
	export class RebaseView {
	    base: string;
	    commits: CommitView[];
	
	    static createFrom(source: any = {}) {
	        return new RebaseView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.base = source["base"];
	        this.commits = this.convertValues(source["commits"], CommitView);
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
	export class ReflogView {
	    hash: string;
	    ref: string;
	    subject: string;
	    when: string;
	
	    static createFrom(source: any = {}) {
	        return new ReflogView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.ref = source["ref"];
	        this.subject = source["subject"];
	        this.when = source["when"];
	    }
	}
	export class RepoGHSignal {
	    repoPath: string;
	    name: string;
	    ci: string;
	    prs: number;
	    issues: number;
	
	    static createFrom(source: any = {}) {
	        return new RepoGHSignal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repoPath = source["repoPath"];
	        this.name = source["name"];
	        this.ci = source["ci"];
	        this.prs = source["prs"];
	        this.issues = source["issues"];
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
	export class StatusFilesView {
	    path: string;
	    staged: boolean;
	    unstaged: boolean;
	    conflict: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StatusFilesView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.staged = source["staged"];
	        this.unstaged = source["unstaged"];
	        this.conflict = source["conflict"];
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
	export class SyncStateView {
	    state: string;
	    lastSyncedUnix: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncStateView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.lastSyncedUnix = source["lastSyncedUnix"];
	        this.error = source["error"];
	    }
	}
	
	export class UnclonedView {
	    id: string;
	    name: string;
	    remote: string;
	    taskCount: number;
	    canClone: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UnclonedView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.remote = source["remote"];
	        this.taskCount = source["taskCount"];
	        this.canClone = source["canClone"];
	    }
	}

}

