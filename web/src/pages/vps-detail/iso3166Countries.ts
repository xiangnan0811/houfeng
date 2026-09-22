/* ISO 3166-1 country/region list ported from the accepted dialog prototype.
 *
 * Codes (249 assigned alpha-2): Debian iso-codes JSON
 *   /usr/share/iso-codes/json/iso_3166-1.json
 * Official English names: same iso-codes file (ISO 3166-1 English short name).
 * Official Chinese names: Debian iso-codes gettext catalog
 *   /usr/share/locale/zh_CN/LC_MESSAGES/iso_3166-1.mo
 *
 * Short UI labels are resolved at runtime with Intl.DisplayNames (zh-CN, en).
 * Do not invent alpha-2 codes.
 */

export type Iso3166Country = {
  code: string
  alpha3: string
  numeric: string
  name: string
  zhOfficial: string
  officialName?: string
  commonName?: string
}

export type CommonCountryGroup = {
  id: string
  label: string
  codes: readonly string[]
}

export const ISO3166_1_COUNTRIES: Iso3166Country[] = [
  {
    "code": "AD",
    "alpha3": "AND",
    "numeric": "020",
    "name": "Andorra",
    "zhOfficial": "安道尔",
    "officialName": "Principality of Andorra"
  },
  {
    "code": "AE",
    "alpha3": "ARE",
    "numeric": "784",
    "name": "United Arab Emirates",
    "zhOfficial": "阿联酋"
  },
  {
    "code": "AF",
    "alpha3": "AFG",
    "numeric": "004",
    "name": "Afghanistan",
    "zhOfficial": "阿富汗",
    "officialName": "Islamic Republic of Afghanistan"
  },
  {
    "code": "AG",
    "alpha3": "ATG",
    "numeric": "028",
    "name": "Antigua and Barbuda",
    "zhOfficial": "安提瓜和巴布达"
  },
  {
    "code": "AI",
    "alpha3": "AIA",
    "numeric": "660",
    "name": "Anguilla",
    "zhOfficial": "安圭拉"
  },
  {
    "code": "AL",
    "alpha3": "ALB",
    "numeric": "008",
    "name": "Albania",
    "zhOfficial": "阿尔巴尼亚",
    "officialName": "Republic of Albania"
  },
  {
    "code": "AM",
    "alpha3": "ARM",
    "numeric": "051",
    "name": "Armenia",
    "zhOfficial": "亚美尼亚",
    "officialName": "Republic of Armenia"
  },
  {
    "code": "AO",
    "alpha3": "AGO",
    "numeric": "024",
    "name": "Angola",
    "zhOfficial": "安哥拉",
    "officialName": "Republic of Angola"
  },
  {
    "code": "AQ",
    "alpha3": "ATA",
    "numeric": "010",
    "name": "Antarctica",
    "zhOfficial": "南极洲"
  },
  {
    "code": "AR",
    "alpha3": "ARG",
    "numeric": "032",
    "name": "Argentina",
    "zhOfficial": "阿根廷",
    "officialName": "Argentine Republic"
  },
  {
    "code": "AS",
    "alpha3": "ASM",
    "numeric": "016",
    "name": "American Samoa",
    "zhOfficial": "美属萨摩亚"
  },
  {
    "code": "AT",
    "alpha3": "AUT",
    "numeric": "040",
    "name": "Austria",
    "zhOfficial": "奥地利",
    "officialName": "Republic of Austria"
  },
  {
    "code": "AU",
    "alpha3": "AUS",
    "numeric": "036",
    "name": "Australia",
    "zhOfficial": "澳大利亚"
  },
  {
    "code": "AW",
    "alpha3": "ABW",
    "numeric": "533",
    "name": "Aruba",
    "zhOfficial": "阿鲁巴"
  },
  {
    "code": "AX",
    "alpha3": "ALA",
    "numeric": "248",
    "name": "Åland Islands",
    "zhOfficial": "奥兰群岛"
  },
  {
    "code": "AZ",
    "alpha3": "AZE",
    "numeric": "031",
    "name": "Azerbaijan",
    "zhOfficial": "阿塞拜疆",
    "officialName": "Republic of Azerbaijan"
  },
  {
    "code": "BA",
    "alpha3": "BIH",
    "numeric": "070",
    "name": "Bosnia and Herzegovina",
    "zhOfficial": "波斯尼亚和黑塞哥维那",
    "officialName": "Republic of Bosnia and Herzegovina"
  },
  {
    "code": "BB",
    "alpha3": "BRB",
    "numeric": "052",
    "name": "Barbados",
    "zhOfficial": "巴巴多斯"
  },
  {
    "code": "BD",
    "alpha3": "BGD",
    "numeric": "050",
    "name": "Bangladesh",
    "zhOfficial": "孟加拉",
    "officialName": "People's Republic of Bangladesh"
  },
  {
    "code": "BE",
    "alpha3": "BEL",
    "numeric": "056",
    "name": "Belgium",
    "zhOfficial": "比利时",
    "officialName": "Kingdom of Belgium"
  },
  {
    "code": "BF",
    "alpha3": "BFA",
    "numeric": "854",
    "name": "Burkina Faso",
    "zhOfficial": "布基纳法索"
  },
  {
    "code": "BG",
    "alpha3": "BGR",
    "numeric": "100",
    "name": "Bulgaria",
    "zhOfficial": "保加利亚",
    "officialName": "Republic of Bulgaria"
  },
  {
    "code": "BH",
    "alpha3": "BHR",
    "numeric": "048",
    "name": "Bahrain",
    "zhOfficial": "巴林",
    "officialName": "Kingdom of Bahrain"
  },
  {
    "code": "BI",
    "alpha3": "BDI",
    "numeric": "108",
    "name": "Burundi",
    "zhOfficial": "布隆迪",
    "officialName": "Republic of Burundi"
  },
  {
    "code": "BJ",
    "alpha3": "BEN",
    "numeric": "204",
    "name": "Benin",
    "zhOfficial": "贝宁",
    "officialName": "Republic of Benin"
  },
  {
    "code": "BL",
    "alpha3": "BLM",
    "numeric": "652",
    "name": "Saint Barthélemy",
    "zhOfficial": "圣巴泰勒米岛"
  },
  {
    "code": "BM",
    "alpha3": "BMU",
    "numeric": "060",
    "name": "Bermuda",
    "zhOfficial": "百慕大"
  },
  {
    "code": "BN",
    "alpha3": "BRN",
    "numeric": "096",
    "name": "Brunei Darussalam",
    "zhOfficial": "文莱"
  },
  {
    "code": "BO",
    "alpha3": "BOL",
    "numeric": "068",
    "name": "Bolivia, Plurinational State of",
    "zhOfficial": "玻利维亚共和国",
    "officialName": "Plurinational State of Bolivia",
    "commonName": "Bolivia"
  },
  {
    "code": "BQ",
    "alpha3": "BES",
    "numeric": "535",
    "name": "Bonaire, Sint Eustatius and Saba",
    "zhOfficial": "博奈尔、圣尤斯特歇斯岛和萨巴",
    "officialName": "Bonaire, Sint Eustatius and Saba"
  },
  {
    "code": "BR",
    "alpha3": "BRA",
    "numeric": "076",
    "name": "Brazil",
    "zhOfficial": "巴西",
    "officialName": "Federative Republic of Brazil"
  },
  {
    "code": "BS",
    "alpha3": "BHS",
    "numeric": "044",
    "name": "Bahamas",
    "zhOfficial": "巴哈马",
    "officialName": "Commonwealth of the Bahamas"
  },
  {
    "code": "BT",
    "alpha3": "BTN",
    "numeric": "064",
    "name": "Bhutan",
    "zhOfficial": "不丹",
    "officialName": "Kingdom of Bhutan"
  },
  {
    "code": "BV",
    "alpha3": "BVT",
    "numeric": "074",
    "name": "Bouvet Island",
    "zhOfficial": "布维群岛"
  },
  {
    "code": "BW",
    "alpha3": "BWA",
    "numeric": "072",
    "name": "Botswana",
    "zhOfficial": "博茨瓦纳",
    "officialName": "Republic of Botswana"
  },
  {
    "code": "BY",
    "alpha3": "BLR",
    "numeric": "112",
    "name": "Belarus",
    "zhOfficial": "白俄罗斯",
    "officialName": "Republic of Belarus"
  },
  {
    "code": "BZ",
    "alpha3": "BLZ",
    "numeric": "084",
    "name": "Belize",
    "zhOfficial": "伯利兹"
  },
  {
    "code": "CA",
    "alpha3": "CAN",
    "numeric": "124",
    "name": "Canada",
    "zhOfficial": "加拿大"
  },
  {
    "code": "CC",
    "alpha3": "CCK",
    "numeric": "166",
    "name": "Cocos (Keeling) Islands",
    "zhOfficial": "科科斯 (基林) 群岛"
  },
  {
    "code": "CD",
    "alpha3": "COD",
    "numeric": "180",
    "name": "Congo, The Democratic Republic of the",
    "zhOfficial": "刚果民主共和国"
  },
  {
    "code": "CF",
    "alpha3": "CAF",
    "numeric": "140",
    "name": "Central African Republic",
    "zhOfficial": "中非共和国"
  },
  {
    "code": "CG",
    "alpha3": "COG",
    "numeric": "178",
    "name": "Congo",
    "zhOfficial": "刚果",
    "officialName": "Republic of the Congo"
  },
  {
    "code": "CH",
    "alpha3": "CHE",
    "numeric": "756",
    "name": "Switzerland",
    "zhOfficial": "瑞士",
    "officialName": "Swiss Confederation"
  },
  {
    "code": "CI",
    "alpha3": "CIV",
    "numeric": "384",
    "name": "Côte d'Ivoire",
    "zhOfficial": "科特迪瓦",
    "officialName": "Republic of Côte d'Ivoire"
  },
  {
    "code": "CK",
    "alpha3": "COK",
    "numeric": "184",
    "name": "Cook Islands",
    "zhOfficial": "库克群岛"
  },
  {
    "code": "CL",
    "alpha3": "CHL",
    "numeric": "152",
    "name": "Chile",
    "zhOfficial": "智利",
    "officialName": "Republic of Chile"
  },
  {
    "code": "CM",
    "alpha3": "CMR",
    "numeric": "120",
    "name": "Cameroon",
    "zhOfficial": "喀麦隆",
    "officialName": "Republic of Cameroon"
  },
  {
    "code": "CN",
    "alpha3": "CHN",
    "numeric": "156",
    "name": "China",
    "zhOfficial": "中国",
    "officialName": "People's Republic of China"
  },
  {
    "code": "CO",
    "alpha3": "COL",
    "numeric": "170",
    "name": "Colombia",
    "zhOfficial": "哥伦比亚",
    "officialName": "Republic of Colombia"
  },
  {
    "code": "CR",
    "alpha3": "CRI",
    "numeric": "188",
    "name": "Costa Rica",
    "zhOfficial": "哥斯达黎加",
    "officialName": "Republic of Costa Rica"
  },
  {
    "code": "CU",
    "alpha3": "CUB",
    "numeric": "192",
    "name": "Cuba",
    "zhOfficial": "古巴",
    "officialName": "Republic of Cuba"
  },
  {
    "code": "CV",
    "alpha3": "CPV",
    "numeric": "132",
    "name": "Cabo Verde",
    "zhOfficial": "佛得角",
    "officialName": "Republic of Cabo Verde"
  },
  {
    "code": "CW",
    "alpha3": "CUW",
    "numeric": "531",
    "name": "Curaçao",
    "zhOfficial": "库拉索",
    "officialName": "Curaçao"
  },
  {
    "code": "CX",
    "alpha3": "CXR",
    "numeric": "162",
    "name": "Christmas Island",
    "zhOfficial": "圣诞岛"
  },
  {
    "code": "CY",
    "alpha3": "CYP",
    "numeric": "196",
    "name": "Cyprus",
    "zhOfficial": "塞浦路斯",
    "officialName": "Republic of Cyprus"
  },
  {
    "code": "CZ",
    "alpha3": "CZE",
    "numeric": "203",
    "name": "Czechia",
    "zhOfficial": "捷克",
    "officialName": "Czech Republic"
  },
  {
    "code": "DE",
    "alpha3": "DEU",
    "numeric": "276",
    "name": "Germany",
    "zhOfficial": "德国",
    "officialName": "Federal Republic of Germany"
  },
  {
    "code": "DJ",
    "alpha3": "DJI",
    "numeric": "262",
    "name": "Djibouti",
    "zhOfficial": "吉布提",
    "officialName": "Republic of Djibouti"
  },
  {
    "code": "DK",
    "alpha3": "DNK",
    "numeric": "208",
    "name": "Denmark",
    "zhOfficial": "丹麦",
    "officialName": "Kingdom of Denmark"
  },
  {
    "code": "DM",
    "alpha3": "DMA",
    "numeric": "212",
    "name": "Dominica",
    "zhOfficial": "多米尼克",
    "officialName": "Commonwealth of Dominica"
  },
  {
    "code": "DO",
    "alpha3": "DOM",
    "numeric": "214",
    "name": "Dominican Republic",
    "zhOfficial": "多米尼加共和国"
  },
  {
    "code": "DZ",
    "alpha3": "DZA",
    "numeric": "012",
    "name": "Algeria",
    "zhOfficial": "阿尔及利亚",
    "officialName": "People's Democratic Republic of Algeria"
  },
  {
    "code": "EC",
    "alpha3": "ECU",
    "numeric": "218",
    "name": "Ecuador",
    "zhOfficial": "厄瓜多尔",
    "officialName": "Republic of Ecuador"
  },
  {
    "code": "EE",
    "alpha3": "EST",
    "numeric": "233",
    "name": "Estonia",
    "zhOfficial": "爱沙尼亚",
    "officialName": "Republic of Estonia"
  },
  {
    "code": "EG",
    "alpha3": "EGY",
    "numeric": "818",
    "name": "Egypt",
    "zhOfficial": "埃及",
    "officialName": "Arab Republic of Egypt"
  },
  {
    "code": "EH",
    "alpha3": "ESH",
    "numeric": "732",
    "name": "Western Sahara",
    "zhOfficial": "西撒哈拉"
  },
  {
    "code": "ER",
    "alpha3": "ERI",
    "numeric": "232",
    "name": "Eritrea",
    "zhOfficial": "厄立特里亚",
    "officialName": "the State of Eritrea"
  },
  {
    "code": "ES",
    "alpha3": "ESP",
    "numeric": "724",
    "name": "Spain",
    "zhOfficial": "西班牙",
    "officialName": "Kingdom of Spain"
  },
  {
    "code": "ET",
    "alpha3": "ETH",
    "numeric": "231",
    "name": "Ethiopia",
    "zhOfficial": "埃塞俄比亚",
    "officialName": "Federal Democratic Republic of Ethiopia"
  },
  {
    "code": "FI",
    "alpha3": "FIN",
    "numeric": "246",
    "name": "Finland",
    "zhOfficial": "芬兰",
    "officialName": "Republic of Finland"
  },
  {
    "code": "FJ",
    "alpha3": "FJI",
    "numeric": "242",
    "name": "Fiji",
    "zhOfficial": "斐济",
    "officialName": "Republic of Fiji"
  },
  {
    "code": "FK",
    "alpha3": "FLK",
    "numeric": "238",
    "name": "Falkland Islands (Malvinas)",
    "zhOfficial": "福克兰群岛 (马尔维纳斯)"
  },
  {
    "code": "FM",
    "alpha3": "FSM",
    "numeric": "583",
    "name": "Micronesia, Federated States of",
    "zhOfficial": "密克罗尼西亚",
    "officialName": "Federated States of Micronesia"
  },
  {
    "code": "FO",
    "alpha3": "FRO",
    "numeric": "234",
    "name": "Faroe Islands",
    "zhOfficial": "法罗群岛"
  },
  {
    "code": "FR",
    "alpha3": "FRA",
    "numeric": "250",
    "name": "France",
    "zhOfficial": "法国",
    "officialName": "French Republic"
  },
  {
    "code": "GA",
    "alpha3": "GAB",
    "numeric": "266",
    "name": "Gabon",
    "zhOfficial": "加蓬",
    "officialName": "Gabonese Republic"
  },
  {
    "code": "GB",
    "alpha3": "GBR",
    "numeric": "826",
    "name": "United Kingdom",
    "zhOfficial": "英国",
    "officialName": "United Kingdom of Great Britain and Northern Ireland"
  },
  {
    "code": "GD",
    "alpha3": "GRD",
    "numeric": "308",
    "name": "Grenada",
    "zhOfficial": "格林纳达"
  },
  {
    "code": "GE",
    "alpha3": "GEO",
    "numeric": "268",
    "name": "Georgia",
    "zhOfficial": "格鲁吉亚"
  },
  {
    "code": "GF",
    "alpha3": "GUF",
    "numeric": "254",
    "name": "French Guiana",
    "zhOfficial": "法属圭亚那"
  },
  {
    "code": "GG",
    "alpha3": "GGY",
    "numeric": "831",
    "name": "Guernsey",
    "zhOfficial": "根西岛"
  },
  {
    "code": "GH",
    "alpha3": "GHA",
    "numeric": "288",
    "name": "Ghana",
    "zhOfficial": "加纳",
    "officialName": "Republic of Ghana"
  },
  {
    "code": "GI",
    "alpha3": "GIB",
    "numeric": "292",
    "name": "Gibraltar",
    "zhOfficial": "直布罗陀"
  },
  {
    "code": "GL",
    "alpha3": "GRL",
    "numeric": "304",
    "name": "Greenland",
    "zhOfficial": "格陵兰"
  },
  {
    "code": "GM",
    "alpha3": "GMB",
    "numeric": "270",
    "name": "Gambia",
    "zhOfficial": "冈比亚",
    "officialName": "Republic of the Gambia"
  },
  {
    "code": "GN",
    "alpha3": "GIN",
    "numeric": "324",
    "name": "Guinea",
    "zhOfficial": "几内亚",
    "officialName": "Republic of Guinea"
  },
  {
    "code": "GP",
    "alpha3": "GLP",
    "numeric": "312",
    "name": "Guadeloupe",
    "zhOfficial": "瓜德罗普"
  },
  {
    "code": "GQ",
    "alpha3": "GNQ",
    "numeric": "226",
    "name": "Equatorial Guinea",
    "zhOfficial": "赤道几内亚",
    "officialName": "Republic of Equatorial Guinea"
  },
  {
    "code": "GR",
    "alpha3": "GRC",
    "numeric": "300",
    "name": "Greece",
    "zhOfficial": "希腊",
    "officialName": "Hellenic Republic"
  },
  {
    "code": "GS",
    "alpha3": "SGS",
    "numeric": "239",
    "name": "South Georgia and the South Sandwich Islands",
    "zhOfficial": "南乔治亚和南桑德韦奇群岛"
  },
  {
    "code": "GT",
    "alpha3": "GTM",
    "numeric": "320",
    "name": "Guatemala",
    "zhOfficial": "瓜地马拉",
    "officialName": "Republic of Guatemala"
  },
  {
    "code": "GU",
    "alpha3": "GUM",
    "numeric": "316",
    "name": "Guam",
    "zhOfficial": "关岛"
  },
  {
    "code": "GW",
    "alpha3": "GNB",
    "numeric": "624",
    "name": "Guinea-Bissau",
    "zhOfficial": "几内亚比绍",
    "officialName": "Republic of Guinea-Bissau"
  },
  {
    "code": "GY",
    "alpha3": "GUY",
    "numeric": "328",
    "name": "Guyana",
    "zhOfficial": "圭亚那",
    "officialName": "Republic of Guyana"
  },
  {
    "code": "HK",
    "alpha3": "HKG",
    "numeric": "344",
    "name": "Hong Kong",
    "zhOfficial": "香港",
    "officialName": "Hong Kong Special Administrative Region of China"
  },
  {
    "code": "HM",
    "alpha3": "HMD",
    "numeric": "334",
    "name": "Heard Island and McDonald Islands",
    "zhOfficial": "赫德岛与麦克唐纳群岛"
  },
  {
    "code": "HN",
    "alpha3": "HND",
    "numeric": "340",
    "name": "Honduras",
    "zhOfficial": "洪都拉斯",
    "officialName": "Republic of Honduras"
  },
  {
    "code": "HR",
    "alpha3": "HRV",
    "numeric": "191",
    "name": "Croatia",
    "zhOfficial": "克罗地亚",
    "officialName": "Republic of Croatia"
  },
  {
    "code": "HT",
    "alpha3": "HTI",
    "numeric": "332",
    "name": "Haiti",
    "zhOfficial": "海地",
    "officialName": "Republic of Haiti"
  },
  {
    "code": "HU",
    "alpha3": "HUN",
    "numeric": "348",
    "name": "Hungary",
    "zhOfficial": "匈牙利",
    "officialName": "Hungary"
  },
  {
    "code": "ID",
    "alpha3": "IDN",
    "numeric": "360",
    "name": "Indonesia",
    "zhOfficial": "印度尼西亚",
    "officialName": "Republic of Indonesia"
  },
  {
    "code": "IE",
    "alpha3": "IRL",
    "numeric": "372",
    "name": "Ireland",
    "zhOfficial": "爱尔兰"
  },
  {
    "code": "IL",
    "alpha3": "ISR",
    "numeric": "376",
    "name": "Israel",
    "zhOfficial": "以色列",
    "officialName": "State of Israel"
  },
  {
    "code": "IM",
    "alpha3": "IMN",
    "numeric": "833",
    "name": "Isle of Man",
    "zhOfficial": "曼岛"
  },
  {
    "code": "IN",
    "alpha3": "IND",
    "numeric": "356",
    "name": "India",
    "zhOfficial": "印度",
    "officialName": "Republic of India"
  },
  {
    "code": "IO",
    "alpha3": "IOT",
    "numeric": "086",
    "name": "British Indian Ocean Territory",
    "zhOfficial": "英属印度洋领地"
  },
  {
    "code": "IQ",
    "alpha3": "IRQ",
    "numeric": "368",
    "name": "Iraq",
    "zhOfficial": "伊拉克",
    "officialName": "Republic of Iraq"
  },
  {
    "code": "IR",
    "alpha3": "IRN",
    "numeric": "364",
    "name": "Iran, Islamic Republic of",
    "zhOfficial": "伊朗伊斯兰共和国",
    "officialName": "Islamic Republic of Iran",
    "commonName": "Iran"
  },
  {
    "code": "IS",
    "alpha3": "ISL",
    "numeric": "352",
    "name": "Iceland",
    "zhOfficial": "冰岛",
    "officialName": "Republic of Iceland"
  },
  {
    "code": "IT",
    "alpha3": "ITA",
    "numeric": "380",
    "name": "Italy",
    "zhOfficial": "意大利",
    "officialName": "Italian Republic"
  },
  {
    "code": "JE",
    "alpha3": "JEY",
    "numeric": "832",
    "name": "Jersey",
    "zhOfficial": "泽西岛"
  },
  {
    "code": "JM",
    "alpha3": "JAM",
    "numeric": "388",
    "name": "Jamaica",
    "zhOfficial": "牙买加"
  },
  {
    "code": "JO",
    "alpha3": "JOR",
    "numeric": "400",
    "name": "Jordan",
    "zhOfficial": "约旦",
    "officialName": "Hashemite Kingdom of Jordan"
  },
  {
    "code": "JP",
    "alpha3": "JPN",
    "numeric": "392",
    "name": "Japan",
    "zhOfficial": "日本"
  },
  {
    "code": "KE",
    "alpha3": "KEN",
    "numeric": "404",
    "name": "Kenya",
    "zhOfficial": "肯尼亚",
    "officialName": "Republic of Kenya"
  },
  {
    "code": "KG",
    "alpha3": "KGZ",
    "numeric": "417",
    "name": "Kyrgyzstan",
    "zhOfficial": "吉尔吉斯坦",
    "officialName": "Kyrgyz Republic"
  },
  {
    "code": "KH",
    "alpha3": "KHM",
    "numeric": "116",
    "name": "Cambodia",
    "zhOfficial": "柬埔塞",
    "officialName": "Kingdom of Cambodia"
  },
  {
    "code": "KI",
    "alpha3": "KIR",
    "numeric": "296",
    "name": "Kiribati",
    "zhOfficial": "基里巴斯",
    "officialName": "Republic of Kiribati"
  },
  {
    "code": "KM",
    "alpha3": "COM",
    "numeric": "174",
    "name": "Comoros",
    "zhOfficial": "科摩罗",
    "officialName": "Union of the Comoros"
  },
  {
    "code": "KN",
    "alpha3": "KNA",
    "numeric": "659",
    "name": "Saint Kitts and Nevis",
    "zhOfficial": "圣基茨和尼维斯"
  },
  {
    "code": "KP",
    "alpha3": "PRK",
    "numeric": "408",
    "name": "Korea, Democratic People's Republic of",
    "zhOfficial": "朝鲜民主主义人民共和国",
    "officialName": "Democratic People's Republic of Korea",
    "commonName": "North Korea"
  },
  {
    "code": "KR",
    "alpha3": "KOR",
    "numeric": "410",
    "name": "Korea, Republic of",
    "zhOfficial": "大韩民国",
    "commonName": "South Korea"
  },
  {
    "code": "KW",
    "alpha3": "KWT",
    "numeric": "414",
    "name": "Kuwait",
    "zhOfficial": "科威特",
    "officialName": "State of Kuwait"
  },
  {
    "code": "KY",
    "alpha3": "CYM",
    "numeric": "136",
    "name": "Cayman Islands",
    "zhOfficial": "开曼群岛"
  },
  {
    "code": "KZ",
    "alpha3": "KAZ",
    "numeric": "398",
    "name": "Kazakhstan",
    "zhOfficial": "哈萨克斯坦",
    "officialName": "Republic of Kazakhstan"
  },
  {
    "code": "LA",
    "alpha3": "LAO",
    "numeric": "418",
    "name": "Lao People's Democratic Republic",
    "zhOfficial": "老挝人民民主共和国",
    "commonName": "Laos"
  },
  {
    "code": "LB",
    "alpha3": "LBN",
    "numeric": "422",
    "name": "Lebanon",
    "zhOfficial": "黎巴嫩",
    "officialName": "Lebanese Republic"
  },
  {
    "code": "LC",
    "alpha3": "LCA",
    "numeric": "662",
    "name": "Saint Lucia",
    "zhOfficial": "圣卢西亚"
  },
  {
    "code": "LI",
    "alpha3": "LIE",
    "numeric": "438",
    "name": "Liechtenstein",
    "zhOfficial": "列支敦士登",
    "officialName": "Principality of Liechtenstein"
  },
  {
    "code": "LK",
    "alpha3": "LKA",
    "numeric": "144",
    "name": "Sri Lanka",
    "zhOfficial": "斯里兰卡",
    "officialName": "Democratic Socialist Republic of Sri Lanka"
  },
  {
    "code": "LR",
    "alpha3": "LBR",
    "numeric": "430",
    "name": "Liberia",
    "zhOfficial": "利比里亚",
    "officialName": "Republic of Liberia"
  },
  {
    "code": "LS",
    "alpha3": "LSO",
    "numeric": "426",
    "name": "Lesotho",
    "zhOfficial": "莱索托",
    "officialName": "Kingdom of Lesotho"
  },
  {
    "code": "LT",
    "alpha3": "LTU",
    "numeric": "440",
    "name": "Lithuania",
    "zhOfficial": "立陶宛",
    "officialName": "Republic of Lithuania"
  },
  {
    "code": "LU",
    "alpha3": "LUX",
    "numeric": "442",
    "name": "Luxembourg",
    "zhOfficial": "卢森堡",
    "officialName": "Grand Duchy of Luxembourg"
  },
  {
    "code": "LV",
    "alpha3": "LVA",
    "numeric": "428",
    "name": "Latvia",
    "zhOfficial": "拉脱维亚",
    "officialName": "Republic of Latvia"
  },
  {
    "code": "LY",
    "alpha3": "LBY",
    "numeric": "434",
    "name": "Libya",
    "zhOfficial": "利比亚",
    "officialName": "Libya"
  },
  {
    "code": "MA",
    "alpha3": "MAR",
    "numeric": "504",
    "name": "Morocco",
    "zhOfficial": "摩洛哥",
    "officialName": "Kingdom of Morocco"
  },
  {
    "code": "MC",
    "alpha3": "MCO",
    "numeric": "492",
    "name": "Monaco",
    "zhOfficial": "摩纳哥",
    "officialName": "Principality of Monaco"
  },
  {
    "code": "MD",
    "alpha3": "MDA",
    "numeric": "498",
    "name": "Moldova, Republic of",
    "zhOfficial": "摩尔多瓦共和国",
    "officialName": "Republic of Moldova",
    "commonName": "Moldova"
  },
  {
    "code": "ME",
    "alpha3": "MNE",
    "numeric": "499",
    "name": "Montenegro",
    "zhOfficial": "黑山",
    "officialName": "Montenegro"
  },
  {
    "code": "MF",
    "alpha3": "MAF",
    "numeric": "663",
    "name": "Saint Martin (French part)",
    "zhOfficial": "法属圣马丁"
  },
  {
    "code": "MG",
    "alpha3": "MDG",
    "numeric": "450",
    "name": "Madagascar",
    "zhOfficial": "马达加斯加",
    "officialName": "Republic of Madagascar"
  },
  {
    "code": "MH",
    "alpha3": "MHL",
    "numeric": "584",
    "name": "Marshall Islands",
    "zhOfficial": "马绍尔群岛",
    "officialName": "Republic of the Marshall Islands"
  },
  {
    "code": "MK",
    "alpha3": "MKD",
    "numeric": "807",
    "name": "North Macedonia",
    "zhOfficial": "北马其顿",
    "officialName": "Republic of North Macedonia"
  },
  {
    "code": "ML",
    "alpha3": "MLI",
    "numeric": "466",
    "name": "Mali",
    "zhOfficial": "马里",
    "officialName": "Republic of Mali"
  },
  {
    "code": "MM",
    "alpha3": "MMR",
    "numeric": "104",
    "name": "Myanmar",
    "zhOfficial": "缅甸",
    "officialName": "Republic of Myanmar"
  },
  {
    "code": "MN",
    "alpha3": "MNG",
    "numeric": "496",
    "name": "Mongolia",
    "zhOfficial": "蒙古"
  },
  {
    "code": "MO",
    "alpha3": "MAC",
    "numeric": "446",
    "name": "Macao",
    "zhOfficial": "澳门",
    "officialName": "Macao Special Administrative Region of China"
  },
  {
    "code": "MP",
    "alpha3": "MNP",
    "numeric": "580",
    "name": "Northern Mariana Islands",
    "zhOfficial": "北马里亚纳群岛",
    "officialName": "Commonwealth of the Northern Mariana Islands"
  },
  {
    "code": "MQ",
    "alpha3": "MTQ",
    "numeric": "474",
    "name": "Martinique",
    "zhOfficial": "马提尼克"
  },
  {
    "code": "MR",
    "alpha3": "MRT",
    "numeric": "478",
    "name": "Mauritania",
    "zhOfficial": "毛里塔尼亚",
    "officialName": "Islamic Republic of Mauritania"
  },
  {
    "code": "MS",
    "alpha3": "MSR",
    "numeric": "500",
    "name": "Montserrat",
    "zhOfficial": "蒙特塞拉特"
  },
  {
    "code": "MT",
    "alpha3": "MLT",
    "numeric": "470",
    "name": "Malta",
    "zhOfficial": "马尔他",
    "officialName": "Republic of Malta"
  },
  {
    "code": "MU",
    "alpha3": "MUS",
    "numeric": "480",
    "name": "Mauritius",
    "zhOfficial": "毛里求斯",
    "officialName": "Republic of Mauritius"
  },
  {
    "code": "MV",
    "alpha3": "MDV",
    "numeric": "462",
    "name": "Maldives",
    "zhOfficial": "马尔代夫",
    "officialName": "Republic of Maldives"
  },
  {
    "code": "MW",
    "alpha3": "MWI",
    "numeric": "454",
    "name": "Malawi",
    "zhOfficial": "马拉维",
    "officialName": "Republic of Malawi"
  },
  {
    "code": "MX",
    "alpha3": "MEX",
    "numeric": "484",
    "name": "Mexico",
    "zhOfficial": "墨西哥",
    "officialName": "United Mexican States"
  },
  {
    "code": "MY",
    "alpha3": "MYS",
    "numeric": "458",
    "name": "Malaysia",
    "zhOfficial": "马来西亚"
  },
  {
    "code": "MZ",
    "alpha3": "MOZ",
    "numeric": "508",
    "name": "Mozambique",
    "zhOfficial": "莫桑比克",
    "officialName": "Republic of Mozambique"
  },
  {
    "code": "NA",
    "alpha3": "NAM",
    "numeric": "516",
    "name": "Namibia",
    "zhOfficial": "纳米比亚",
    "officialName": "Republic of Namibia"
  },
  {
    "code": "NC",
    "alpha3": "NCL",
    "numeric": "540",
    "name": "New Caledonia",
    "zhOfficial": "新喀里多尼亚"
  },
  {
    "code": "NE",
    "alpha3": "NER",
    "numeric": "562",
    "name": "Niger",
    "zhOfficial": "尼日尔",
    "officialName": "Republic of the Niger"
  },
  {
    "code": "NF",
    "alpha3": "NFK",
    "numeric": "574",
    "name": "Norfolk Island",
    "zhOfficial": "诺福克岛"
  },
  {
    "code": "NG",
    "alpha3": "NGA",
    "numeric": "566",
    "name": "Nigeria",
    "zhOfficial": "尼日利亚",
    "officialName": "Federal Republic of Nigeria"
  },
  {
    "code": "NI",
    "alpha3": "NIC",
    "numeric": "558",
    "name": "Nicaragua",
    "zhOfficial": "尼加拉瓜",
    "officialName": "Republic of Nicaragua"
  },
  {
    "code": "NL",
    "alpha3": "NLD",
    "numeric": "528",
    "name": "Netherlands",
    "zhOfficial": "荷兰",
    "officialName": "Kingdom of the Netherlands"
  },
  {
    "code": "NO",
    "alpha3": "NOR",
    "numeric": "578",
    "name": "Norway",
    "zhOfficial": "挪威",
    "officialName": "Kingdom of Norway"
  },
  {
    "code": "NP",
    "alpha3": "NPL",
    "numeric": "524",
    "name": "Nepal",
    "zhOfficial": "尼泊尔",
    "officialName": "Federal Democratic Republic of Nepal"
  },
  {
    "code": "NR",
    "alpha3": "NRU",
    "numeric": "520",
    "name": "Nauru",
    "zhOfficial": "瑙鲁",
    "officialName": "Republic of Nauru"
  },
  {
    "code": "NU",
    "alpha3": "NIU",
    "numeric": "570",
    "name": "Niue",
    "zhOfficial": "纽埃",
    "officialName": "Niue"
  },
  {
    "code": "NZ",
    "alpha3": "NZL",
    "numeric": "554",
    "name": "New Zealand",
    "zhOfficial": "新西兰"
  },
  {
    "code": "OM",
    "alpha3": "OMN",
    "numeric": "512",
    "name": "Oman",
    "zhOfficial": "阿曼",
    "officialName": "Sultanate of Oman"
  },
  {
    "code": "PA",
    "alpha3": "PAN",
    "numeric": "591",
    "name": "Panama",
    "zhOfficial": "巴拿马",
    "officialName": "Republic of Panama"
  },
  {
    "code": "PE",
    "alpha3": "PER",
    "numeric": "604",
    "name": "Peru",
    "zhOfficial": "秘鲁",
    "officialName": "Republic of Peru"
  },
  {
    "code": "PF",
    "alpha3": "PYF",
    "numeric": "258",
    "name": "French Polynesia",
    "zhOfficial": "法属玻利尼西亚"
  },
  {
    "code": "PG",
    "alpha3": "PNG",
    "numeric": "598",
    "name": "Papua New Guinea",
    "zhOfficial": "巴布亚新几内亚",
    "officialName": "Independent State of Papua New Guinea"
  },
  {
    "code": "PH",
    "alpha3": "PHL",
    "numeric": "608",
    "name": "Philippines",
    "zhOfficial": "菲律宾",
    "officialName": "Republic of the Philippines"
  },
  {
    "code": "PK",
    "alpha3": "PAK",
    "numeric": "586",
    "name": "Pakistan",
    "zhOfficial": "巴基斯坦",
    "officialName": "Islamic Republic of Pakistan"
  },
  {
    "code": "PL",
    "alpha3": "POL",
    "numeric": "616",
    "name": "Poland",
    "zhOfficial": "波兰",
    "officialName": "Republic of Poland"
  },
  {
    "code": "PM",
    "alpha3": "SPM",
    "numeric": "666",
    "name": "Saint Pierre and Miquelon",
    "zhOfficial": "圣皮埃尔和密克隆"
  },
  {
    "code": "PN",
    "alpha3": "PCN",
    "numeric": "612",
    "name": "Pitcairn",
    "zhOfficial": "皮特凯恩"
  },
  {
    "code": "PR",
    "alpha3": "PRI",
    "numeric": "630",
    "name": "Puerto Rico",
    "zhOfficial": "波多黎各"
  },
  {
    "code": "PS",
    "alpha3": "PSE",
    "numeric": "275",
    "name": "Palestine, State of",
    "zhOfficial": "巴勒斯坦",
    "officialName": "the State of Palestine"
  },
  {
    "code": "PT",
    "alpha3": "PRT",
    "numeric": "620",
    "name": "Portugal",
    "zhOfficial": "葡萄牙",
    "officialName": "Portuguese Republic"
  },
  {
    "code": "PW",
    "alpha3": "PLW",
    "numeric": "585",
    "name": "Palau",
    "zhOfficial": "帕劳",
    "officialName": "Republic of Palau"
  },
  {
    "code": "PY",
    "alpha3": "PRY",
    "numeric": "600",
    "name": "Paraguay",
    "zhOfficial": "巴拉圭",
    "officialName": "Republic of Paraguay"
  },
  {
    "code": "QA",
    "alpha3": "QAT",
    "numeric": "634",
    "name": "Qatar",
    "zhOfficial": "卡塔尔",
    "officialName": "State of Qatar"
  },
  {
    "code": "RE",
    "alpha3": "REU",
    "numeric": "638",
    "name": "Réunion",
    "zhOfficial": "留尼汪"
  },
  {
    "code": "RO",
    "alpha3": "ROU",
    "numeric": "642",
    "name": "Romania",
    "zhOfficial": "罗马尼亚"
  },
  {
    "code": "RS",
    "alpha3": "SRB",
    "numeric": "688",
    "name": "Serbia",
    "zhOfficial": "塞尔维亚",
    "officialName": "Republic of Serbia"
  },
  {
    "code": "RU",
    "alpha3": "RUS",
    "numeric": "643",
    "name": "Russian Federation",
    "zhOfficial": "俄罗斯"
  },
  {
    "code": "RW",
    "alpha3": "RWA",
    "numeric": "646",
    "name": "Rwanda",
    "zhOfficial": "卢旺达",
    "officialName": "Rwandese Republic"
  },
  {
    "code": "SA",
    "alpha3": "SAU",
    "numeric": "682",
    "name": "Saudi Arabia",
    "zhOfficial": "沙特阿拉伯",
    "officialName": "Kingdom of Saudi Arabia"
  },
  {
    "code": "SB",
    "alpha3": "SLB",
    "numeric": "090",
    "name": "Solomon Islands",
    "zhOfficial": "所罗门群岛"
  },
  {
    "code": "SC",
    "alpha3": "SYC",
    "numeric": "690",
    "name": "Seychelles",
    "zhOfficial": "塞舌尔",
    "officialName": "Republic of Seychelles"
  },
  {
    "code": "SD",
    "alpha3": "SDN",
    "numeric": "729",
    "name": "Sudan",
    "zhOfficial": "苏丹",
    "officialName": "Republic of the Sudan"
  },
  {
    "code": "SE",
    "alpha3": "SWE",
    "numeric": "752",
    "name": "Sweden",
    "zhOfficial": "瑞典",
    "officialName": "Kingdom of Sweden"
  },
  {
    "code": "SG",
    "alpha3": "SGP",
    "numeric": "702",
    "name": "Singapore",
    "zhOfficial": "新加坡",
    "officialName": "Republic of Singapore"
  },
  {
    "code": "SH",
    "alpha3": "SHN",
    "numeric": "654",
    "name": "Saint Helena, Ascension and Tristan da Cunha",
    "zhOfficial": "圣赫勒拿-阿森松-特里斯坦-达库尼亚"
  },
  {
    "code": "SI",
    "alpha3": "SVN",
    "numeric": "705",
    "name": "Slovenia",
    "zhOfficial": "斯洛文尼亚",
    "officialName": "Republic of Slovenia"
  },
  {
    "code": "SJ",
    "alpha3": "SJM",
    "numeric": "744",
    "name": "Svalbard and Jan Mayen",
    "zhOfficial": "斯瓦尔巴和扬马延"
  },
  {
    "code": "SK",
    "alpha3": "SVK",
    "numeric": "703",
    "name": "Slovakia",
    "zhOfficial": "斯洛伐克",
    "officialName": "Slovak Republic"
  },
  {
    "code": "SL",
    "alpha3": "SLE",
    "numeric": "694",
    "name": "Sierra Leone",
    "zhOfficial": "塞拉利昂",
    "officialName": "Republic of Sierra Leone"
  },
  {
    "code": "SM",
    "alpha3": "SMR",
    "numeric": "674",
    "name": "San Marino",
    "zhOfficial": "圣马力诺",
    "officialName": "Republic of San Marino"
  },
  {
    "code": "SN",
    "alpha3": "SEN",
    "numeric": "686",
    "name": "Senegal",
    "zhOfficial": "塞内加尔",
    "officialName": "Republic of Senegal"
  },
  {
    "code": "SO",
    "alpha3": "SOM",
    "numeric": "706",
    "name": "Somalia",
    "zhOfficial": "索马里",
    "officialName": "Federal Republic of Somalia"
  },
  {
    "code": "SR",
    "alpha3": "SUR",
    "numeric": "740",
    "name": "Suriname",
    "zhOfficial": "苏里南",
    "officialName": "Republic of Suriname"
  },
  {
    "code": "SS",
    "alpha3": "SSD",
    "numeric": "728",
    "name": "South Sudan",
    "zhOfficial": "南苏丹",
    "officialName": "Republic of South Sudan"
  },
  {
    "code": "ST",
    "alpha3": "STP",
    "numeric": "678",
    "name": "Sao Tome and Principe",
    "zhOfficial": "圣多美和普林西比",
    "officialName": "Democratic Republic of Sao Tome and Principe"
  },
  {
    "code": "SV",
    "alpha3": "SLV",
    "numeric": "222",
    "name": "El Salvador",
    "zhOfficial": "萨尔瓦多",
    "officialName": "Republic of El Salvador"
  },
  {
    "code": "SX",
    "alpha3": "SXM",
    "numeric": "534",
    "name": "Sint Maarten (Dutch part)",
    "zhOfficial": "荷属圣马丁",
    "officialName": "Sint Maarten (Dutch part)"
  },
  {
    "code": "SY",
    "alpha3": "SYR",
    "numeric": "760",
    "name": "Syrian Arab Republic",
    "zhOfficial": "阿拉伯叙利亚共和国",
    "commonName": "Syria"
  },
  {
    "code": "SZ",
    "alpha3": "SWZ",
    "numeric": "748",
    "name": "Eswatini",
    "zhOfficial": "斯威士兰",
    "officialName": "Kingdom of Eswatini"
  },
  {
    "code": "TC",
    "alpha3": "TCA",
    "numeric": "796",
    "name": "Turks and Caicos Islands",
    "zhOfficial": "特克斯和凯科斯群岛"
  },
  {
    "code": "TD",
    "alpha3": "TCD",
    "numeric": "148",
    "name": "Chad",
    "zhOfficial": "乍得",
    "officialName": "Republic of Chad"
  },
  {
    "code": "TF",
    "alpha3": "ATF",
    "numeric": "260",
    "name": "French Southern Territories",
    "zhOfficial": "法属南半球领地"
  },
  {
    "code": "TG",
    "alpha3": "TGO",
    "numeric": "768",
    "name": "Togo",
    "zhOfficial": "多哥",
    "officialName": "Togolese Republic"
  },
  {
    "code": "TH",
    "alpha3": "THA",
    "numeric": "764",
    "name": "Thailand",
    "zhOfficial": "泰国",
    "officialName": "Kingdom of Thailand"
  },
  {
    "code": "TJ",
    "alpha3": "TJK",
    "numeric": "762",
    "name": "Tajikistan",
    "zhOfficial": "塔吉克斯坦",
    "officialName": "Republic of Tajikistan"
  },
  {
    "code": "TK",
    "alpha3": "TKL",
    "numeric": "772",
    "name": "Tokelau",
    "zhOfficial": "托克劳"
  },
  {
    "code": "TL",
    "alpha3": "TLS",
    "numeric": "626",
    "name": "Timor-Leste",
    "zhOfficial": "东帝汶",
    "officialName": "Democratic Republic of Timor-Leste"
  },
  {
    "code": "TM",
    "alpha3": "TKM",
    "numeric": "795",
    "name": "Turkmenistan",
    "zhOfficial": "土库曼斯坦"
  },
  {
    "code": "TN",
    "alpha3": "TUN",
    "numeric": "788",
    "name": "Tunisia",
    "zhOfficial": "突尼斯",
    "officialName": "Republic of Tunisia"
  },
  {
    "code": "TO",
    "alpha3": "TON",
    "numeric": "776",
    "name": "Tonga",
    "zhOfficial": "汤加",
    "officialName": "Kingdom of Tonga"
  },
  {
    "code": "TR",
    "alpha3": "TUR",
    "numeric": "792",
    "name": "Türkiye",
    "zhOfficial": "土耳其",
    "officialName": "Republic of Türkiye"
  },
  {
    "code": "TT",
    "alpha3": "TTO",
    "numeric": "780",
    "name": "Trinidad and Tobago",
    "zhOfficial": "特里尼达和多巴哥",
    "officialName": "Republic of Trinidad and Tobago"
  },
  {
    "code": "TV",
    "alpha3": "TUV",
    "numeric": "798",
    "name": "Tuvalu",
    "zhOfficial": "图瓦卢"
  },
  {
    "code": "TW",
    "alpha3": "TWN",
    "numeric": "158",
    "name": "Taiwan, Province of China",
    "zhOfficial": "中国台湾省",
    "officialName": "Taiwan, Province of China",
    "commonName": "Taiwan"
  },
  {
    "code": "TZ",
    "alpha3": "TZA",
    "numeric": "834",
    "name": "Tanzania, United Republic of",
    "zhOfficial": "坦桑尼亚",
    "officialName": "United Republic of Tanzania",
    "commonName": "Tanzania"
  },
  {
    "code": "UA",
    "alpha3": "UKR",
    "numeric": "804",
    "name": "Ukraine",
    "zhOfficial": "乌克兰"
  },
  {
    "code": "UG",
    "alpha3": "UGA",
    "numeric": "800",
    "name": "Uganda",
    "zhOfficial": "乌干达",
    "officialName": "Republic of Uganda"
  },
  {
    "code": "UM",
    "alpha3": "UMI",
    "numeric": "581",
    "name": "United States Minor Outlying Islands",
    "zhOfficial": "美国本土外小岛屿"
  },
  {
    "code": "US",
    "alpha3": "USA",
    "numeric": "840",
    "name": "United States",
    "zhOfficial": "美国",
    "officialName": "United States of America"
  },
  {
    "code": "UY",
    "alpha3": "URY",
    "numeric": "858",
    "name": "Uruguay",
    "zhOfficial": "乌拉圭",
    "officialName": "Eastern Republic of Uruguay"
  },
  {
    "code": "UZ",
    "alpha3": "UZB",
    "numeric": "860",
    "name": "Uzbekistan",
    "zhOfficial": "乌兹别克斯坦",
    "officialName": "Republic of Uzbekistan"
  },
  {
    "code": "VA",
    "alpha3": "VAT",
    "numeric": "336",
    "name": "Holy See (Vatican City State)",
    "zhOfficial": "梵蒂冈"
  },
  {
    "code": "VC",
    "alpha3": "VCT",
    "numeric": "670",
    "name": "Saint Vincent and the Grenadines",
    "zhOfficial": "圣文森特和格林纳丁斯"
  },
  {
    "code": "VE",
    "alpha3": "VEN",
    "numeric": "862",
    "name": "Venezuela, Bolivarian Republic of",
    "zhOfficial": "委内瑞拉玻利瓦尔共和国",
    "officialName": "Bolivarian Republic of Venezuela",
    "commonName": "Venezuela"
  },
  {
    "code": "VG",
    "alpha3": "VGB",
    "numeric": "092",
    "name": "Virgin Islands, British",
    "zhOfficial": "英属维尔京群岛",
    "officialName": "British Virgin Islands"
  },
  {
    "code": "VI",
    "alpha3": "VIR",
    "numeric": "850",
    "name": "Virgin Islands, U.S.",
    "zhOfficial": "美属维尔京群岛",
    "officialName": "Virgin Islands of the United States"
  },
  {
    "code": "VN",
    "alpha3": "VNM",
    "numeric": "704",
    "name": "Viet Nam",
    "zhOfficial": "越南",
    "officialName": "Socialist Republic of Viet Nam",
    "commonName": "Vietnam"
  },
  {
    "code": "VU",
    "alpha3": "VUT",
    "numeric": "548",
    "name": "Vanuatu",
    "zhOfficial": "瓦努阿图",
    "officialName": "Republic of Vanuatu"
  },
  {
    "code": "WF",
    "alpha3": "WLF",
    "numeric": "876",
    "name": "Wallis and Futuna",
    "zhOfficial": "瓦利斯和富图纳"
  },
  {
    "code": "WS",
    "alpha3": "WSM",
    "numeric": "882",
    "name": "Samoa",
    "zhOfficial": "萨摩亚",
    "officialName": "Independent State of Samoa"
  },
  {
    "code": "YE",
    "alpha3": "YEM",
    "numeric": "887",
    "name": "Yemen",
    "zhOfficial": "也门",
    "officialName": "Republic of Yemen"
  },
  {
    "code": "YT",
    "alpha3": "MYT",
    "numeric": "175",
    "name": "Mayotte",
    "zhOfficial": "马约特"
  },
  {
    "code": "ZA",
    "alpha3": "ZAF",
    "numeric": "710",
    "name": "South Africa",
    "zhOfficial": "南非",
    "officialName": "Republic of South Africa"
  },
  {
    "code": "ZM",
    "alpha3": "ZMB",
    "numeric": "894",
    "name": "Zambia",
    "zhOfficial": "赞比亚",
    "officialName": "Republic of Zambia"
  },
  {
    "code": "ZW",
    "alpha3": "ZWE",
    "numeric": "716",
    "name": "Zimbabwe",
    "zhOfficial": "津巴布韦",
    "officialName": "Republic of Zimbabwe"
  }
]

/* Browse grouping only. Codes must exist in ISO3166_1_COUNTRIES above.
 * Common set mirrors web/src/lib/assetOptions.ts COMMON_COUNTRY_OPTIONS; only geography is added here.
 */
export const COMMON_COUNTRY_GROUPS: readonly CommonCountryGroup[] = [
  {
    "id": "asia",
    "label": "亚洲",
    "codes": [
      "HK",
      "SG",
      "JP",
      "TW",
      "KR",
      "CN",
      "MY",
      "TH",
      "IN"
    ]
  },
  {
    "id": "europe",
    "label": "欧洲",
    "codes": [
      "GB",
      "DE",
      "FR",
      "NL"
    ]
  },
  {
    "id": "americas",
    "label": "美洲",
    "codes": [
      "US",
      "CA",
      "BR"
    ]
  },
  {
    "id": "oceania",
    "label": "大洋洲",
    "codes": [
      "AU"
    ]
  }
]
