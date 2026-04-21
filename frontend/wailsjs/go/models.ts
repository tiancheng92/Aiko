export namespace config {
	
	export class Config {
	    LLMBaseURL: string;
	    LLMAPIKey: string;
	    LLMModel: string;
	    EmbeddingModel: string;
	    EmbeddingDim: number;
	    SystemPrompt: string;
	    ShortTermLimit: number;
	    SkillsDir: string;
	    Hotkey: string;
	    BallPositionX: number;
	    BallPositionY: number;
	    BubblePositionX: number;
	    BubblePositionY: number;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.LLMBaseURL = source["LLMBaseURL"];
	        this.LLMAPIKey = source["LLMAPIKey"];
	        this.LLMModel = source["LLMModel"];
	        this.EmbeddingModel = source["EmbeddingModel"];
	        this.EmbeddingDim = source["EmbeddingDim"];
	        this.SystemPrompt = source["SystemPrompt"];
	        this.ShortTermLimit = source["ShortTermLimit"];
	        this.SkillsDir = source["SkillsDir"];
	        this.Hotkey = source["Hotkey"];
	        this.BallPositionX = source["BallPositionX"];
	        this.BallPositionY = source["BallPositionY"];
	        this.BubblePositionX = source["BubblePositionX"];
	        this.BubblePositionY = source["BubblePositionY"];
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

