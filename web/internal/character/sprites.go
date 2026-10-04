package character

// ドット絵（16×16）。1文字が1ドットで、色は palette で決める。「.」は透明、「E」は目。
// 表情（目を閉じる・笑う）は目の「E」を置き換えて作るので、目は縦2ドットで描く。
//
// キャラクターはこのアプリのオリジナル（鳥の「ピコ」）。既存の作品のキャラクターは使わない。

var palette = map[byte]string{
	'W': "#f8f0d8", // たまごの殻・おなか
	'Y': "#f8d838", // 黄
	'O': "#f89838", // くちばし・あし
	'B': "#4c8dff", // 青い体
	'b': "#2f5fbf", // つばさ（濃い青）
	'R': "#f85858", // とさか・宝石
	'S': "#c8c8d8", // かぶと
	's': "#e8e8f8", // 剣の刃
	'h': "#a8783a", // 剣のつか
	'C': "#c83a5a", // マント
	'E': "#000000", // 目
}

// stages はレベルごとの姿。stages[0] が Lv1。
var stages = []stage{
	{
		Name: "タマゴ",
		Pixels: []string{
			"................",
			"......WWWW......",
			".....WWWWWW.....",
			"....WWWWWWWW....",
			"...WWYYWWWWWW...",
			"...WYYYWWWWYW...",
			"..WWWYWWWWWYYW..",
			"..WWWEWWWWEWWW..",
			"..WWWEWWWWEWWW..",
			"..WWWWWWWWWWWW..",
			"..WYYWWWWWWWWW..",
			"..WYYWWWWWYYWW..",
			"...WWWWWWWYYW...",
			"....WWWWWWWW....",
			".....WWWWWW.....",
			"................",
		},
	},
	{
		Name: "ヒヨコ",
		Pixels: []string{
			"................",
			".......YY.......",
			"......YYYY......",
			"....YYYYYYYY....",
			"...YYYYYYYYYY...",
			"...YYEYYYYEYY...",
			"..YYYEYYYYEYYY..",
			"..YYYYYOOYYYYY..",
			"..YYYYOOOOYYYY..",
			"..YYYYYYYYYYYY..",
			"..YYYYYYYYYYYY..",
			"...YYYYYYYYYY...",
			"....YYYYYYYY....",
			".....O....O.....",
			"....OO....OO....",
			"................",
		},
	},
	{
		Name: "コトリ",
		Pixels: []string{
			".......RR.......",
			"......RRR.......",
			".....BBBBBB.....",
			"....BBBBBBBB....",
			"...BBBBBBBBBB...",
			"...BBEBBBBEBB...",
			"..BBBEBBBBEBBB..",
			"..BBBBBOOBBBBB..",
			"..bBBBOOOOBBBb..",
			"..bbBWWWWWWBbb..",
			"..bbBWWWWWWBbb..",
			"...bBWWWWWWBb...",
			"....BBBBBBBB....",
			".....O....O.....",
			"....OO....OO....",
			"................",
		},
	},
	{
		Name: "ナイト",
		Pixels: []string{
			"......SSSS......",
			".....SSSSSS...s.",
			"....SSSSSSSS..s.",
			"...SSSSSSSSSS.s.",
			"...BBEBBBBEBB.s.",
			"..BBBEBBBBEBBBs.",
			"..BBBBBOOBBBBBs.",
			"..bBBBOOOOBBBhhh",
			"..bbBWWWWWWBbbh.",
			"..bbBWWWWWWBbb..",
			"...bBWWWWWWBb...",
			"....BBBBBBBB....",
			".....O....O.....",
			"....OO....OO....",
			"................",
			"................",
		},
	},
	{
		Name: "キング",
		Pixels: []string{
			"....Y..YY..Y....",
			"....YYYYYYYY....",
			"....YRYYYYRY....",
			"...BBBBBBBBBB...",
			"..BBBBBBBBBBBB..",
			"..BBBEBBBBEBBB..",
			".CBBBEBBBBEBBBC.",
			".CBBBBBOOBBBBBC.",
			".CbBBBOOOOBBBbC.",
			".CbbBWWWWWWBbbC.",
			".CCbBWWWWWWBbCC.",
			".CCCBWWWWWWBCCC.",
			"..CCCBBBBBBCCC..",
			".....O....O.....",
			"....OO....OO....",
			"................",
		},
	},
}
