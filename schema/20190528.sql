alter table active_checks add state_since timestamp with time zone default now() not null;
