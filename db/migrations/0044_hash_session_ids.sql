do $$
begin
	if exists (
		select 1
		from information_schema.columns
		where table_schema = current_schema()
		  and table_name = 'sessions'
		  and column_name = 'session_id'
	) and not exists (
		select 1
		from information_schema.columns
		where table_schema = current_schema()
		  and table_name = 'sessions'
		  and column_name = 'session_id_hash'
	) then
		delete from sessions;
		alter table sessions rename column session_id to session_id_hash;
	end if;
end $$;

