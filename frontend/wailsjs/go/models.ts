export namespace config {
	
	export class Config {
	    LLMBaseURL: string;
	    LLMAPIKey: string;
	    LLMModel: string;
	    LLMProvider: string;
	    EmbeddingModel: string;
	    Live2DModel: string;
	    EmbeddingDim: number;
	    SystemPrompt: string;
	    ShortTermLimit: number;
	    NudgeInterval: number;
	    SMSWatcherEnabled: boolean;
	    VoiceAutoSend: boolean;
	    SoundsEnabled: boolean;
	    SkillsDirs: string[];
	    PetSize: number;
	    ChatWidth: number;
	    ChatHeight: number;
	    ActiveProfileID: number;
	    TTSModel: string;
	    TTSVoice: string;
	    TTSSpeed: number;
	    TTSAutoPlay: boolean;
	    TTSSummarizeThreshold: number;
	    TTSBackend: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.LLMBaseURL = source["LLMBaseURL"];
	        this.LLMAPIKey = source["LLMAPIKey"];
	        this.LLMModel = source["LLMModel"];
	        this.LLMProvider = source["LLMProvider"];
	        this.EmbeddingModel = source["EmbeddingModel"];
	        this.Live2DModel = source["Live2DModel"];
	        this.EmbeddingDim = source["EmbeddingDim"];
	        this.SystemPrompt = source["SystemPrompt"];
	        this.ShortTermLimit = source["ShortTermLimit"];
	        this.NudgeInterval = source["NudgeInterval"];
	        this.SMSWatcherEnabled = source["SMSWatcherEnabled"];
	        this.VoiceAutoSend = source["VoiceAutoSend"];
	        this.SoundsEnabled = source["SoundsEnabled"];
	        this.SkillsDirs = source["SkillsDirs"];
	        this.PetSize = source["PetSize"];
	        this.ChatWidth = source["ChatWidth"];
	        this.ChatHeight = source["ChatHeight"];
	        this.ActiveProfileID = source["ActiveProfileID"];
	        this.TTSModel = source["TTSModel"];
	        this.TTSVoice = source["TTSVoice"];
	        this.TTSSpeed = source["TTSSpeed"];
	        this.TTSAutoPlay = source["TTSAutoPlay"];
	        this.TTSSummarizeThreshold = source["TTSSummarizeThreshold"];
	        this.TTSBackend = source["TTSBackend"];
	    }
	}
	export class ModelProfile {
	    id: number;
	    name: string;
	    provider: string;
	    base_url: string;
	    api_key: string;
	    model: string;
	    embedding_model: string;
	    embedding_dim: number;
	    tts_model: string;
	    tts_voice: string;
	    tts_speed: number;
	    tts_backend: string;
	
	    static createFrom(source: any = {}) {
	        return new ModelProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.base_url = source["base_url"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.embedding_model = source["embedding_model"];
	        this.embedding_dim = source["embedding_dim"];
	        this.tts_model = source["tts_model"];
	        this.tts_voice = source["tts_voice"];
	        this.tts_speed = source["tts_speed"];
	        this.tts_backend = source["tts_backend"];
	    }
	}

}

export namespace frontend {
	
	export class FileFilter {
	    DisplayName: string;
	    Pattern: string;
	
	    static createFrom(source: any = {}) {
	        return new FileFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DisplayName = source["DisplayName"];
	        this.Pattern = source["Pattern"];
	    }
	}

}

export namespace main {
	
	export class MousePosition {
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new MousePosition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class ScreenInfo {
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new ScreenInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}

}

export namespace mcp {
	
	export class ServerConfig {
	    id: number;
	    name: string;
	    transport: string;
	    command: string;
	    args: string[];
	    url: string;
	    headers: Record<string, string>;
	    enabled: boolean;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.transport = source["transport"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.url = source["url"];
	        this.headers = source["headers"];
	        this.enabled = source["enabled"];
	        this.created_at = source["created_at"];
	    }
	}

}

export namespace memory {
	
	export class Message {
	    ID: number;
	    Role: string;
	    Content: string;
	    CreatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Role = source["Role"];
	        this.Content = source["Content"];
	        this.CreatedAt = source["CreatedAt"];
	    }
	}

}

export namespace proactive {
	
	export class Item {
	    ID: number;
	    // Go type: time
	    TriggerAt: any;
	    Prompt: string;
	    // Go type: time
	    CreatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.TriggerAt = this.convertValues(source["TriggerAt"], null);
	        this.Prompt = source["Prompt"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
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

export namespace scheduler {
	
	export class Job {
	    ID: number;
	    Name: string;
	    Description: string;
	    Schedule: string;
	    Prompt: string;
	    Enabled: boolean;
	    LastRun?: string;
	    CreatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Description = source["Description"];
	        this.Schedule = source["Schedule"];
	        this.Prompt = source["Prompt"];
	        this.Enabled = source["Enabled"];
	        this.LastRun = source["LastRun"];
	        this.CreatedAt = source["CreatedAt"];
	    }
	}

}

export namespace tools {
	
	export class PermissionRow {
	    ToolName: string;
	    Level: string;
	    Granted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PermissionRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ToolName = source["ToolName"];
	        this.Level = source["Level"];
	        this.Granted = source["Granted"];
	    }
	}

}

