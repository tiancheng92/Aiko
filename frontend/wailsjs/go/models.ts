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
	    SkillsDirs: string[];
	    PetSize: number;
	    ChatWidth: number;
	    ChatHeight: number;
	    ActiveProfileID: number;
	
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
	        this.SkillsDirs = source["SkillsDirs"];
	        this.PetSize = source["PetSize"];
	        this.ChatWidth = source["ChatWidth"];
	        this.ChatHeight = source["ChatHeight"];
	        this.ActiveProfileID = source["ActiveProfileID"];
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

