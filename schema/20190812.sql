create table checkers(
  id serial not null primary key,
  name text not null unique,
  description text
);

insert into checkers(name, description) values ('moncheck', 'moncheck provides a nagios compatible API to run checks. It calls binaries, which control the alarm state by their exit code.');

alter table checks add checker_id integer not null default 1
  references checkers(id) on delete cascade;
alter table checks alter checker_id drop default;
alter table active_checks add checker_id integer not null default 1
  references checkers(id) on delete cascade;
alter table active_checks alter checker_id drop default;
