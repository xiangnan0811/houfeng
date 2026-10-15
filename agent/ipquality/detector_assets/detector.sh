#!/usr/bin/env bash
# xykt/IPQuality v2026-09-16, commit 2384a67c756eb35231f5982b34731e522be3653e.
# AGPL-3.0; see LICENSE and MODIFICATIONS.md. Audited local fork revision 1.
# Derived functions: db_maxmind, db_ipinfo, MediaUnlockTest_Netflix,
# MediaUnlockTest_YouTube_Premium, MediaUnlockTest_PrimeVideo_Region, OpenAITest.
# HTTP/static extraction is delegated, but ALL business decisions remain here.
set -euo pipefail
(( BASH_VERSINFO[0] >= 4 )) || exit 70
module=$1; IP=$2; family=$3; FETCH=$4
pin=2384a67c756eb35231f5982b34731e522be3653e
status=unknown; region=''; unlock_type=''; rule=''; error_code=invalid_response
samples='[]'; diagnostics='[]'

fetch() {
    local endpoint=$1
    "$FETCH" ip-quality-fetch "$endpoint" "$IP" "$family" >/dev/null || return 1
    FACT="$endpoint.json"
}

# Static jq programs only. Response strings are data, never interpolated code.
# A valid boolean false is distinct from an absent/null/nonboolean value.
classify() {
    local endpoint=$1 program=$2
    if ! fetch "$endpoint"; then
        state=unknown; code=detector_fetch_failed; country=''
    else
        local base='def country: if type=="string" and test("^[A-Za-z]{2}$") then ascii_upcase else "" end;
          def unique_country: map(country) | if length>0 and all(.!="") and (unique|length)==1 then .[0] else "" end;
          def ok: .http_status>=200 and .http_status<300;
          def failure: if .error_code!="" then .error_code elif .http_status==429 or .http_status>=500 then "http_status" elif .challenge then "challenge_detected" elif .login then "invalid_response" else "" end;
          def result($s;$c;$r): {state:$s,error_code:$c,region:$r};'
        decision=$(jq -c --arg endpoint "$endpoint" "$base $program" "$FACT")
        state=$(jq -r '.state' <<<"$decision")
        code=$(jq -r '.error_code' <<<"$decision")
        country=$(jq -r '.region' <<<"$decision")
    fi
    local http=0
    [[ ! -f "$endpoint.json" ]] || http=$(jq -r '.http_status' "$endpoint.json")
    diagnostics=$(jq -c --arg endpoint "$endpoint" --arg outcome "$state" --arg code "$code" --arg region "$country" --argjson http "$http" '.+[{endpoint:$endpoint,outcome:$outcome,error_code:$code,region:$region,http_status:$http}]' <<<"$diagnostics")
}

# Upstream used absence of Oh no as success. Require page identity and positive
# title content; country denial has precedence over Android metadata.
netflix_sample() {
    local id=$1
    classify "netflix-$id" '
      (.values.netflixCountry // [] | unique_country) as $r |
      if failure!="" then result("unknown";failure;"")
      elif (.metas["al:android:app_name"] // "" | ascii_downcase)!="netflix" or
           ((.metas["al:android:url"] // "" | rtrimstr("/")) as $url |
              ($endpoint|ltrimstr("netflix-")) as $id |
              $url != ("nflx://www.netflix.com/title/"+$id) and $url != ("nflx://title/"+$id))
        then result("unknown";"invalid_response";"")
      elif (.text | ascii_downcase | contains("this title currently isn’t available to watch in your country") or contains("this title currently isn\u0027t available to watch in your country"))
        then result("restricted";"";$r)
      elif ok and (.text|length)>0 and .has_heading
        then result("positive";"";$r)
      else result("unknown";"invalid_response";"") end'
}
MediaUnlockTest_Netflix() {
    netflix_sample 81280792; local first=$state first_region=$country
    netflix_sample 70143836; local second=$state
    region=$country; [[ -n $region ]] || region=$first_region
    [[ -z $first_region || -z $country || $first_region == "$country" ]] || region=''
    if [[ $first == positive || $second == positive ]]; then
        status=unlocked; unlock_type=catalog_sample; rule=netflix_title_catalog; error_code=''
    elif [[ $first == restricted && $second == restricted ]]; then
        netflix_sample 80057281
        case $state in
            positive) status=partial; unlock_type=originals_only; rule=netflix_originals_only; error_code='' ;;
            restricted) status=blocked; unlock_type=catalog_unavailable; rule=netflix_catalog_restricted; error_code='' ;;
            *) region=''; error_code=${code:-invalid_response} ;;
        esac
    else region=''; error_code=${code:-invalid_response}; fi
}
MediaUnlockTest_YouTube_Premium() {
    classify youtube-premium '
      (.values.contentRegion // [] | unique_country) as $primary |
      (if $primary!="" then $primary else (.values.INNERTUBE_CONTEXT_GL // [] | unique_country) end) as $r |
      if failure!="" then result("unknown";failure;"")
      elif (.title|ascii_downcase|contains("youtube")|not) then result("unknown";"invalid_response";"")
      elif (.text|ascii_downcase|contains("premium is not available in your country")) then result("restricted";"";$r)
      elif ok and any(.youtube_headers[]?.subtitles[]?.content; type=="string" and (ascii_downcase|contains("ad-free")))
           and any(.youtube_offers[]?.offerCards[]?.lpOfferCardViewModel; type=="object" and length>0)
        then result("positive";"";$r)
      else result("unknown";"invalid_response";"") end'
    region=$country; unlock_type=premium_offer
    case $state in positive) status=unlocked; rule=youtube_premium_offer; error_code='' ;; restricted) status=blocked; rule=youtube_premium_country_restriction; error_code='' ;; *) error_code=$code; unlock_type='' ;; esac
}
MediaUnlockTest_PrimeVideo_Region() {
    classify amazon-prime-video '
      (.values.currentTerritory // [] | unique_country) as $r |
      if failure!="" then result("unknown";failure;"")
      elif (.title|ascii_downcase|contains("prime video")|not) then result("unknown";"invalid_response";"")
      elif any(.values.isServiceRestricted[]?; .==true) or (.text|ascii_downcase|contains("prime video is not available in your country"))
        then result("restricted";"";$r)
      elif ok and $r!="" then result("positive";"";$r)
      else result("unknown";"invalid_response";"") end'
    region=$country; unlock_type=territory_offer
    case $state in positive) status=unlocked; rule=prime_current_territory; error_code='' ;; restricted) status=blocked; rule=prime_service_restricted; error_code='' ;; *) error_code=$code; unlock_type='' ;; esac
}
OpenAITest() {
    local filter='def denied: any([.json.error.code,.json.error.type,.json.code,.json.type][]; .=="unsupported_country" or .=="unsupported_country_region_territory");
      if failure!="" then result("unknown";failure;"")
      elif denied then result("restricted";"";"")
      elif ok and ((.json.cookie_consent_required|type)=="boolean") then result("positive";"";"")
      else result("unknown";(if .http_status==403 and .json.type=="dc" then "client_access_denied" else "invalid_response" end);"") end'
    classify openai-cookie "$filter"; local web=$state
    classify openai-ios 'def denied: any([.json.error.code,.json.error.type,.json.code,.json.type][]; .=="unsupported_country" or .=="unsupported_country_region_territory");
      if failure!="" then result("unknown";failure;"")
      elif denied then result("restricted";"";"")
      else result("unknown";(if .http_status==403 and .json.type=="dc" then "client_access_denied" else "invalid_response" end);"") end'
    if [[ $web == positive || $state == positive ]]; then
        status=unlocked; rule=openai_anonymous_region_access; error_code=''; unlock_type=region_access
        if [[ $web == restricted || $state == restricted ]]; then status=partial; rule=openai_region_conflict; unlock_type=conflicting_region_evidence; fi
    elif [[ $web == restricted || $state == restricted ]]; then status=blocked; rule=openai_unsupported_country; error_code=''; unlock_type=region_access
    else error_code=${code:-invalid_response}; fi
}

# Adapted upstream field paths. Anonymous IPinfo's abuse country is NOT a
# registered country. The immutable report keeps its classification provenance.
db_maxmind() {
    classify geo-maxmind 'if failure!="" then result("unknown";failure;"") elif ok and (.json|type)=="object" and (.json.Country.IsoCode|country)!="" then result("positive";"";(.json.Country.IsoCode|country)) else result("unknown";"invalid_response";"") end'
    [[ $state == positive ]] || return 0
    local sample
    sample=$(jq -c --arg ip "$IP" 'def cc: if type=="string" and test("^[A-Za-z]{2}$") then ascii_upcase else "" end;
      def txt: if type=="string" then . else "" end;
      .json as $j | {provider:"maxmind",country_code:($j.Country.IsoCode|cc),registered_country_code:($j.Country.RegisteredCountry.IsoCode|cc),city:($j.City.Name|txt),region:($j.City.Subdivisions[0].Name|txt),postal_code:($j.City.PostalCode|txt),timezone:($j.City.Location.TimeZone|txt),asn:($j.ASN.AutonomousSystemNumber|if type=="number" then tostring else txt end),as_name:($j.ASN.AutonomousSystemOrganization|txt)}' "$FACT")
    samples=$(jq -c --argjson s "$sample" '.+[$s]' <<<"$samples")
}
db_ipinfo() {
    classify geo-ipinfo 'if failure!="" then result("unknown";failure;"") elif ok and (.json.data|type)=="object" and (.json.data.country|country)!="" then result("positive";"";(.json.data.country|country)) else result("unknown";"invalid_response";"") end'
    [[ $state == positive ]] || return 0
    local sample
    sample=$(jq -c 'def cc: if type=="string" and test("^[A-Za-z]{2}$") then ascii_upcase else "" end;
      def txt: if type=="string" then . else "" end;
      .json.data as $j | {provider:"ipinfo",country_code:($j.country|cc),abuse_contact_country:($j.abuse.country|cc),city:($j.city|txt),region:($j.region|txt),postal_code:($j.postal|txt),timezone:($j.timezone|txt),asn:($j.asn.asn|txt),as_name:($j.asn.name|txt),usage_type:($j.asn.type|txt),company_type:($j.company.type|txt)}' "$FACT")
    samples=$(jq -c --argjson s "$sample" '.+[$s]' <<<"$samples")
}
case $module in
 netflix) MediaUnlockTest_Netflix ;;
 youtube-premium) MediaUnlockTest_YouTube_Premium ;;
 amazon-prime-video) MediaUnlockTest_PrimeVideo_Region ;;
 chatgpt) OpenAITest ;;
 geo) db_maxmind
      if ! jq -e 'any(.[]; .country_code!="" and .registered_country_code!="")' >/dev/null <<<"$samples"; then db_ipinfo; fi ;;
 *) exit 64 ;;
esac
jq -nc --arg detector_module "$module" --arg ip "$IP" --argjson family "$family" --arg pin "$pin" --arg status "$status" --arg region "$region" --arg scope "$unlock_type" --arg rule "$rule" --arg code "$error_code" --argjson samples "$samples" --argjson diagnostics "$diagnostics" '{schema_revision:1,module:$detector_module,target_ip:$ip,ip_version:$family,upstream_commit:$pin,fork_revision:1,result:{status:$status,region:$region,unlock_type:$scope,rule:$rule,error_code:$code,samples:$samples},diagnostics:$diagnostics}'
