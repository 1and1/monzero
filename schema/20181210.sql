create table public.mappings(
  id serial primary key,
  name text not null,
  description text not null
);

create table public.mapping_level(
  mapping_id integer not null,
  source int not null,
  target int not null,
  title text not null,
  color text not null,
  unique(mapping_id, source)
);

CREATE TABLE public.notifier (
    id serial NOT NULL primary key,
    name text NOT NULL,
    settings jsonb not null default '{}'::jsonb
);

CREATE TABLE public.groups (
    id serial NOT NULL,
    name text NOT NULL
);

CREATE TABLE public.nodes (
    id bigserial NOT NULL primary key,
    mapping_id int references mappings(id),
    name text NOT NULL,
    updated timestamp with time zone DEFAULT now() NOT NULL,
    created timestamp with time zone DEFAULT now() NOT NULL,
    message text NOT NULL
);

CREATE TABLE public.nodes_groups (
    node_id bigint NOT NULL,
    group_id integer NOT NULL,
    unique(node_id, group_id)
);

CREATE TABLE public.commands (
    id serial NOT NULL primary key,
    name text NOT NULL unique,
    command text NOT NULL,
    updated timestamp with time zone DEFAULT now() NOT NULL,
    created timestamp with time zone DEFAULT now() NOT NULL,
    message text NOT NULL
);

CREATE TABLE public.checks (
    id bigserial NOT NULL primary key,
    node_id integer not null references nodes(id) on delete cascade,
    command_id integer not null references commands(id) on delete restrict,
    mapping_id int references mappings(id),
    intval interval DEFAULT '00:05:00'::interval NOT NULL,
    options jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated timestamp with time zone DEFAULT now() NOT NULL,
    last_refresh timestamp with time zone,
    enabled boolean DEFAULT true NOT NULL,
    message text NOT NULL,
    unique(node_id, command_id, options)
);

CREATE TABLE public.active_checks (
    check_id bigint NOT NULL unique references checks(id) on delete cascade,
    mapping_id int not null references mappings(id),
    cmdline text[] NOT NULL,
    next_time timestamp with time zone DEFAULT now() NOT NULL,
    states integer[] DEFAULT ARRAY[0] NOT NULL,
    intval interval NOT NULL,
    enabled boolean NOT NULL,
    notice text,
    msg text NOT NULL,
    acknowledged boolean DEFAULT false NOT NULL
);

create table checks_notify(
  check_id bigint not null references checks(id),
  notifier_id bigint not null references notifier(id),
  enabled bool not null default true,
  unique(check_id, notifier_id)
);

CREATE TABLE public.notifications (
    id bigserial NOT NULL primary key,
    check_id bigint NOT NULL references checks(id) on delete cascade,
    mapping_id integer not null references mappings(id),
    notifier_id integer not null references notifier(id),
    states integer[] NOT NULL,
    output text,
    inserted timestamp with time zone DEFAULT now() NOT NULL,
    sent timestamp with time zone
);


CREATE INDEX ON public.active_checks USING btree (next_time) WHERE enabled;

CREATE INDEX ON public.checks USING btree (command_id);

CREATE INDEX ON public.checks USING btree (node_id);

CREATE INDEX ON public.checks USING btree (updated, last_refresh NULLS FIRST);

CREATE INDEX ON public.notifications USING btree (check_id, inserted DESC);

CREATE INDEX ON public.notifications USING btree (inserted) WHERE (sent IS NULL);
