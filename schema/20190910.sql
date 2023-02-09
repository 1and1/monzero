-- add check instance name field
alter table checks add name text not null default 'none';
alter table checks alter name drop default;
